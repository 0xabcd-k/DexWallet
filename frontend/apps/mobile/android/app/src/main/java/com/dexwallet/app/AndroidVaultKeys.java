package com.dexwallet.app;

import android.os.Build;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import java.io.IOException;
import java.security.GeneralSecurityException;
import java.security.KeyStore;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;

final class AndroidVaultKeys implements WalletVaultStore.Keys {
    static final int AUTH_VALIDITY_SECONDS = 30;
    private static final String PROVIDER = "AndroidKeyStore";
    private static final String PREFIX = "dexwallet.wallet-vault.v1.";
    private KeyStore store() throws GeneralSecurityException, IOException {
        KeyStore store = KeyStore.getInstance(PROVIDER);
        store.load(null);
        return store;
    }

    @Override
    public SecretKey create(String id) throws GeneralSecurityException, IOException {
        // Only called when no record exists. Clear an orphan from an interrupted creation.
        remove(id);
        KeyGenParameterSpec.Builder spec = new KeyGenParameterSpec.Builder(PREFIX + id,
            KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
            .setKeySize(256).setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setRandomizedEncryptionRequired(true).setUserAuthenticationRequired(true);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            spec.setUserAuthenticationParameters(AUTH_VALIDITY_SECONDS, KeyProperties.AUTH_DEVICE_CREDENTIAL);
        } else {
            spec.setUserAuthenticationValidityDurationSeconds(AUTH_VALIDITY_SECONDS);
            spec.setInvalidatedByBiometricEnrollment(false);
        }
        KeyGenerator generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, PROVIDER);
        generator.init(spec.build());
        return generator.generateKey();
    }

    @Override
    public SecretKey find(String id) throws GeneralSecurityException, IOException {
        return (SecretKey) store().getKey(PREFIX + id, null);
    }

    @Override
    public void remove(String id) throws GeneralSecurityException, IOException { store().deleteEntry(PREFIX + id); }
}
