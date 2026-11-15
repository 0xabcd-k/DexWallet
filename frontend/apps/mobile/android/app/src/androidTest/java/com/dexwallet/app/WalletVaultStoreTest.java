package com.dexwallet.app;

import static org.junit.Assert.*;
import android.content.Context;
import androidx.test.platform.app.InstrumentationRegistry;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import java.io.File;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.util.HashMap;
import java.util.Map;
import java.util.UUID;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class WalletVaultStoreTest {
    private File directory;
    private WalletVaultStore store;
    private TestKeys keys;
    private final WalletVaultStore.Record a = record("user-a", "wallet-a");
    private static final byte[] SECRET = "non-funds-vault-test-fixture".getBytes(StandardCharsets.UTF_8);
    // Fake only the keystore boundary here; real Android authentication is tested separately.
    private static final class TestKeys implements WalletVaultStore.Keys {
        final Map<String, SecretKey> values = new HashMap<>();
        @Override public SecretKey create(String id) throws java.security.GeneralSecurityException {
            KeyGenerator generator = KeyGenerator.getInstance("AES"); generator.init(256);
            SecretKey key = generator.generateKey(); values.put(id, key); return key;
        }
        @Override public SecretKey find(String id) { return values.get(id); }
        @Override public void remove(String id) { values.remove(id); }
    }
    private static WalletVaultStore.Record record(String user, String wallet) {
        try { return new WalletVaultStore.Record(user, wallet); } catch (Exception e) { throw new AssertionError(e); }
    }
    @Before public void setUp() {
        Context context = InstrumentationRegistry.getInstrumentation().getTargetContext();
        directory = new File(context.getNoBackupFilesDir(), "vault-test-" + UUID.randomUUID());
        keys = new TestKeys(); store = new WalletVaultStore(directory, keys);
    }
    @After public void cleanUp() throws Exception {
        File[] files = directory.listFiles();
        if (files != null) for (File file : files) Files.deleteIfExists(file.toPath());
        Files.deleteIfExists(directory.toPath());
    }
    private File file(WalletVaultStore.Record record) { return new File(directory, record.id + ".vault"); }
    private interface Action { void run() throws Exception; }
    private void fails(WalletVaultStore.Code code, Action action) throws Exception {
        try { action.run(); fail("Expected " + code); }
        catch (WalletVaultStore.Failure failure) { assertEquals(code, failure.code); }
    }
    @Test public void encryptedRecordSurvivesReopen() throws Exception {
        store.put(a, SECRET);
        assertFalse(new String(Files.readAllBytes(file(a).toPath()), StandardCharsets.ISO_8859_1).contains(new String(SECRET, StandardCharsets.UTF_8)));
        assertArrayEquals(SECRET, new WalletVaultStore(directory, keys).get(a));
    }
    @Test public void duplicateCannotReplaceSecret() throws Exception {
        store.put(a, SECRET);
        fails(WalletVaultStore.Code.EXISTS, () -> store.put(a, "replacement".getBytes(StandardCharsets.UTF_8)));
        assertArrayEquals(SECRET, store.get(a));
    }
    @Test public void separatesUsersAndWallets() throws Exception {
        WalletVaultStore.Record b = record("user-b", "wallet-a"), c = record("user-a", "wallet-b");
        store.put(a, SECRET);
        assertFalse(store.exists(b)); assertFalse(store.exists(c));
        fails(WalletVaultStore.Code.NOT_FOUND, () -> store.get(b));
        store.put(b, new byte[]{1}); store.put(c, new byte[]{2}); store.remove(a);
        assertArrayEquals(new byte[]{1}, store.get(b)); assertArrayEquals(new byte[]{2}, store.get(c));
    }
    @Test public void rejectsTamperedCiphertext() throws Exception {
        store.put(a, SECRET); byte[] bytes = Files.readAllBytes(file(a).toPath());
        bytes[bytes.length - 1] ^= 1; Files.write(file(a).toPath(), bytes);
        fails(WalletVaultStore.Code.CORRUPTED, () -> store.get(a));
        assertTrue(store.exists(a));
    }
    @Test public void rejectsMovedRecordEvenWithSameKey() throws Exception {
        store.put(a, SECRET); WalletVaultStore.Record b = record("user-b", "wallet-a");
        Files.copy(file(a).toPath(), file(b).toPath()); keys.values.put(b.id, keys.values.get(a.id));
        fails(WalletVaultStore.Code.CORRUPTED, () -> store.get(b));
    }
    @Test public void missingKeyDoesNotRegenerateOrOverwrite() throws Exception {
        store.put(a, SECRET); keys.values.clear();
        fails(WalletVaultStore.Code.KEY_UNAVAILABLE, () -> store.get(a));
        fails(WalletVaultStore.Code.EXISTS, () -> store.put(a, SECRET));
        assertTrue(keys.values.isEmpty());
    }
    @Test public void rejectsBadFormatAndOversizedRecord() throws Exception {
        store.put(a, SECRET);
        byte[] bytes = Files.readAllBytes(file(a).toPath()); bytes[0] = 99;
        Files.write(file(a).toPath(), bytes); fails(WalletVaultStore.Code.CORRUPTED, () -> store.get(a));
        Files.write(file(a).toPath(), new byte[9000]); fails(WalletVaultStore.Code.CORRUPTED, () -> store.get(a));
    }
    @Test public void removeIsRepeatableAndRemovesKey() throws Exception {
        store.put(a, SECRET); store.remove(a); store.remove(a);
        assertFalse(store.exists(a)); assertTrue(keys.values.isEmpty());
        fails(WalletVaultStore.Code.NOT_FOUND, () -> store.get(a));
    }
    @Test public void interruptedCreationCannotBeOverwritten() throws Exception {
        assertTrue(directory.mkdirs()); Files.write(new File(file(a) + ".new").toPath(), new byte[]{1});
        assertTrue(store.exists(a)); fails(WalletVaultStore.Code.EXISTS, () -> store.put(a, SECRET));
        store.remove(a); assertFalse(store.exists(a)); store.put(a, SECRET); assertArrayEquals(SECRET, store.get(a));
    }
    @Test public void rejectsInvalidIdentityAndSecretBeforeCreatingKeys() throws Exception {
        fails(WalletVaultStore.Code.INVALID_INPUT, () -> new WalletVaultStore.Record("../outside", "wallet"));
        fails(WalletVaultStore.Code.INVALID_INPUT, () -> store.put(a, new byte[0]));
        fails(WalletVaultStore.Code.INVALID_INPUT, () -> store.put(a, new byte[4097]));
        assertTrue(keys.values.isEmpty()); assertFalse(directory.exists());
    }
    @Test public void storageFailureDoesNotReportSuccessfulCreation() throws Exception {
        Files.write(directory.toPath(), new byte[]{1});
        fails(WalletVaultStore.Code.STORAGE_FAILED, () -> store.put(a, SECRET));
        assertTrue(keys.values.isEmpty());
    }
}
