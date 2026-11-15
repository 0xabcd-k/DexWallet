package com.dexwallet.app;

import static org.junit.Assert.*;
import android.app.KeyguardManager;
import android.content.Context;
import android.security.keystore.KeyInfo;
import android.security.keystore.KeyProperties;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.util.UUID;
import javax.crypto.SecretKey;
import javax.crypto.SecretKeyFactory;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class AndroidVaultKeysTest {
    @Test public void requiresDeviceAuthenticationAndDoesNotExportKey() throws Exception {
        Context context = InstrumentationRegistry.getInstrumentation().getTargetContext();
        assertTrue("Configure the isolated emulator screen lock before this test",
            ((KeyguardManager) context.getSystemService(Context.KEYGUARD_SERVICE)).isDeviceSecure());
        String id = "test-" + UUID.randomUUID();
        AndroidVaultKeys keys = new AndroidVaultKeys();
        try {
            SecretKey key = keys.create(id);
            assertNull(key.getEncoded());
            KeyInfo info = (KeyInfo) SecretKeyFactory.getInstance("AES", "AndroidKeyStore").getKeySpec(key, KeyInfo.class);
            assertTrue(info.isUserAuthenticationRequired());
            assertEquals(AndroidVaultKeys.AUTH_VALIDITY_SECONDS, info.getUserAuthenticationValidityDurationSeconds());
            assertEquals(256, info.getKeySize());
            if (android.os.Build.VERSION.SDK_INT >= 30) assertEquals(KeyProperties.AUTH_DEVICE_CREDENTIAL, info.getUserAuthenticationType());
            Thread.sleep((AndroidVaultKeys.AUTH_VALIDITY_SECONDS + 1L) * 1000L);
            try {
                javax.crypto.Cipher cipher = javax.crypto.Cipher.getInstance("AES/GCM/NoPadding");
                cipher.init(javax.crypto.Cipher.ENCRYPT_MODE, key);
                fail("Expired device authentication must prevent encryption");
            } catch (android.security.keystore.UserNotAuthenticatedException expected) {
                // Checked against the real Android Keystore, not the fake store boundary.
            }
        } finally { keys.remove(id); }
    }
}
