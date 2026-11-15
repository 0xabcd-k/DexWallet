package com.dexwallet.app;

import android.util.AtomicFile;
import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.security.GeneralSecurityException;
import java.security.MessageDigest;
import java.util.Arrays;
import javax.crypto.Cipher;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/** Local encrypted records. Callers serialize operations and obtain device authentication. */
final class WalletVaultStore {
    static final int VERSION = 1;
    static final int MAX_SECRET_BYTES = 4096;
    private static final int IV_BYTES = 12;
    private static final int TAG_BITS = 128;
    private static final String CIPHER = "AES/GCM/NoPadding";

    enum Code { INVALID_INPUT, EXISTS, NOT_FOUND, CORRUPTED, KEY_UNAVAILABLE, STORAGE_FAILED }
    static final class Failure extends Exception {
        final Code code;
        Failure(Code code) { super(code.name()); this.code = code; }
        Failure(Code code, Throwable cause) { super(code.name(), cause); this.code = code; }
    }

    interface Keys {
        SecretKey create(String id) throws GeneralSecurityException, IOException;
        SecretKey find(String id) throws GeneralSecurityException, IOException;
        void remove(String id) throws GeneralSecurityException, IOException;
    }

    static final class Record {
        final String id;
        final byte[] aad;
        Record(String userIdx, String walletId) throws Failure {
            if (userIdx == null || walletId == null || !userIdx.matches("[A-Za-z0-9_-]{1,128}") ||
                !walletId.matches("[A-Za-z0-9_-]{1,128}")) throw new Failure(Code.INVALID_INPUT);
            aad = ("DexWallet:wallet-vault:" + VERSION + ":" + userIdx + ":" + walletId).getBytes(StandardCharsets.UTF_8);
            try {
                byte[] digest = MessageDigest.getInstance("SHA-256").digest(aad);
                StringBuilder value = new StringBuilder();
                for (byte b : digest) value.append(String.format(java.util.Locale.ROOT, "%02x", b & 255));
                id = value.toString();
            } catch (GeneralSecurityException e) { throw new IllegalStateException(e); }
        }
    }

    private final File directory;
    private final Keys keys;
    WalletVaultStore(File directory, Keys keys) { this.directory = directory; this.keys = keys; }

    private AtomicFile file(Record record) { return new AtomicFile(new File(directory, record.id + ".vault")); }
    boolean exists(Record record) {
        File base = file(record).getBaseFile();
        // Interrupted writes must not be mistaken for an unused wallet slot.
        return base.exists() || new File(base + ".bak").exists() || new File(base + ".new").exists();
    }

    void put(Record record, byte[] secret) throws Failure, GeneralSecurityException, IOException {
        if (secret == null || secret.length == 0 || secret.length > MAX_SECRET_BYTES) throw new Failure(Code.INVALID_INPUT);
        if (exists(record)) throw new Failure(Code.EXISTS);
        if (!directory.isDirectory() && !directory.mkdirs()) throw new Failure(Code.STORAGE_FAILED);
        AtomicFile file = file(record);
        boolean createdKey = false;
        try {
            SecretKey key = keys.create(record.id);
            createdKey = true;
            Cipher cipher = Cipher.getInstance(CIPHER);
            cipher.init(Cipher.ENCRYPT_MODE, key);
            cipher.updateAAD(record.aad);
            byte[] encrypted = cipher.doFinal(secret);
            if (cipher.getIV().length != IV_BYTES) throw new GeneralSecurityException("Unexpected GCM IV size");
            byte[] encoded = ByteBuffer.allocate(1 + IV_BYTES + encrypted.length)
                .put((byte) VERSION).put(cipher.getIV()).put(encrypted).array();
            FileOutputStream stream = null;
            try {
                stream = file.startWrite();
                stream.write(encoded);
                // AtomicFile logs some sync/rename failures instead of throwing.
                stream.getFD().sync();
                file.finishWrite(stream);
                stream = null;
                if (!Arrays.equals(encoded, file.readFully())) throw new IOException("Vault write verification failed");
            } finally { if (stream != null) file.failWrite(stream); }
        } catch (GeneralSecurityException | IOException | RuntimeException e) {
            if (createdKey) {
                file.delete();
                try { keys.remove(record.id); } catch (Exception cleanup) { e.addSuppressed(cleanup); }
            }
            throw e;
        }
    }

    byte[] get(Record record) throws Failure, GeneralSecurityException, IOException {
        if (!exists(record)) throw new Failure(Code.NOT_FOUND);
        byte[] encoded;
        try (java.io.InputStream in = file(record).openRead()) {
            // Bound reads before allocating an arbitrarily large damaged record.
            java.io.ByteArrayOutputStream data = new java.io.ByteArrayOutputStream();
            byte[] buffer = new byte[1024];
            int count;
            while ((count = in.read(buffer)) != -1) {
                if (data.size() + count > 1 + IV_BYTES + MAX_SECRET_BYTES + TAG_BITS / 8) throw new Failure(Code.CORRUPTED);
                data.write(buffer, 0, count);
            }
            encoded = data.toByteArray();
        } catch (java.io.FileNotFoundException e) { throw new Failure(Code.CORRUPTED, e); }
        if (encoded.length <= 1 + IV_BYTES + TAG_BITS / 8 || encoded[0] != VERSION) throw new Failure(Code.CORRUPTED);
        SecretKey key = keys.find(record.id);
        if (key == null) throw new Failure(Code.KEY_UNAVAILABLE);
        Cipher cipher = Cipher.getInstance(CIPHER);
        cipher.init(Cipher.DECRYPT_MODE, key, new GCMParameterSpec(TAG_BITS, encoded, 1, IV_BYTES));
        cipher.updateAAD(record.aad);
        try { return cipher.doFinal(encoded, 1 + IV_BYTES, encoded.length - 1 - IV_BYTES); }
        catch (javax.crypto.BadPaddingException e) { throw new Failure(Code.CORRUPTED, e); }
    }

    void remove(Record record) throws Failure, GeneralSecurityException, IOException {
        file(record).delete();
        if (exists(record)) throw new Failure(Code.STORAGE_FAILED);
        keys.remove(record.id);
    }
}
