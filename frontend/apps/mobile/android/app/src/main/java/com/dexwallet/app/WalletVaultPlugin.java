package com.dexwallet.app;

import android.app.Activity;
import android.app.KeyguardManager;
import android.content.Context;
import android.content.Intent;
import android.security.keystore.KeyPermanentlyInvalidatedException;
import android.security.keystore.UserNotAuthenticatedException;
import android.util.Log;
import androidx.activity.result.ActivityResult;
import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.ActivityCallback;
import com.getcapacitor.annotation.CapacitorPlugin;
import java.io.File;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

@CapacitorPlugin(name = "WalletVault")
public class WalletVaultPlugin extends Plugin {
    private enum Operation { PUT, GET, REMOVE }
    private static final String TAG = "DexWalletVault";
    private final ExecutorService worker = Executors.newSingleThreadExecutor();
    private WalletVaultStore vault;
    private KeyguardManager keyguard;
    // Access only under this plugin's monitor. Never persisted in a Bundle.
    private Request pending;
    private boolean destroyed;
    private static final class Request {
        final PluginCall call;
        final Operation operation;
        final WalletVaultStore.Record record;
        final byte[] secret;
        boolean executing;
        Request(PluginCall call, Operation operation, WalletVaultStore.Record record, byte[] secret) {
            this.call = call; this.operation = operation; this.record = record; this.secret = secret;
        }
        void clear() { if (secret != null) Arrays.fill(secret, (byte) 0); }
    }

    @Override public void load() {
        keyguard = (KeyguardManager) getContext().getSystemService(Context.KEYGUARD_SERVICE);
        vault = new WalletVaultStore(new File(getContext().getNoBackupFilesDir(), "wallet-vault-v1"), new AndroidVaultKeys());
    }

    @PluginMethod public void inspect(PluginCall call) {
        try {
            WalletVaultStore.Record record = record(call);
            JSObject result = new JSObject();
            result.put("exists", vault.exists(record));
            result.put("deviceSecure", keyguard.isDeviceSecure());
            call.resolve(result);
        } catch (Exception e) { fail(call, e); }
    }
    @PluginMethod public void put(PluginCall call) { authenticate(call, Operation.PUT); }
    @PluginMethod public void get(PluginCall call) { authenticate(call, Operation.GET); }
    @PluginMethod public void remove(PluginCall call) { authenticate(call, Operation.REMOVE); }

    private WalletVaultStore.Record record(PluginCall call) throws WalletVaultStore.Failure {
        return new WalletVaultStore.Record(call.getString("userIdx"), call.getString("walletId"));
    }

    private synchronized void authenticate(PluginCall call, Operation operation) {
        String value = call.getString("secret");
        // Capacitor saves Activity-call arguments across Activity/process recreation.
        // Do not let an input secret become saved instance state.
        call.getData().remove("secret");
        if (pending != null) { call.reject("Wallet vault is busy", "VAULT_BUSY"); return; }
        if (destroyed) { call.reject("Wallet vault is unavailable", "VAULT_UNAVAILABLE"); return; }
        if (!keyguard.isDeviceSecure()) { call.reject("Set up the device screen lock first", "VAULT_DEVICE_LOCK_REQUIRED"); return; }
        byte[] secret = null;
        try {
            WalletVaultStore.Record record = record(call);
            if (operation == Operation.PUT) {
                if (value == null || value.isEmpty() || value.length() > WalletVaultStore.MAX_SECRET_BYTES ||
                    !StandardCharsets.UTF_8.newEncoder().canEncode(value)) throw new WalletVaultStore.Failure(WalletVaultStore.Code.INVALID_INPUT);
                secret = value.getBytes(StandardCharsets.UTF_8);
                if (secret.length > WalletVaultStore.MAX_SECRET_BYTES) throw new WalletVaultStore.Failure(WalletVaultStore.Code.INVALID_INPUT);
                if (vault.exists(record)) throw new WalletVaultStore.Failure(WalletVaultStore.Code.EXISTS);
            }
            int description = switch (operation) {
                case PUT -> R.string.vault_store_description;
                case GET -> R.string.vault_read_description;
                case REMOVE -> R.string.vault_remove_description;
            };
            Intent intent = keyguard.createConfirmDeviceCredentialIntent(
                getContext().getString(R.string.vault_unlock_title), getContext().getString(description));
            if (intent == null) throw new IllegalStateException("Device credential screen unavailable");
            pending = new Request(call, operation, record, secret);
            getActivity().runOnUiThread(() -> {
                synchronized (WalletVaultPlugin.this) {
                    if (pending == null || destroyed) return;
                    try { startActivityForResult(call, intent, "credentialResult"); }
                    catch (Exception e) { pending.clear(); pending = null; fail(call, e); }
                }
            });
        } catch (Exception e) {
            if (secret != null) Arrays.fill(secret, (byte) 0);
            fail(call, e);
        }
    }

    @ActivityCallback
    private synchronized void credentialResult(PluginCall call, ActivityResult result) {
        if (pending == null || call == null || !pending.call.getCallbackId().equals(call.getCallbackId())) {
            if (call != null) call.reject("Restart the interrupted wallet operation", "VAULT_INTERRUPTED");
            return;
        }
        Request request = pending;
        if (destroyed || request.executing) {
            call.reject("Restart the interrupted wallet operation", "VAULT_INTERRUPTED");
            return;
        }
        if (result.getResultCode() != Activity.RESULT_OK) {
            request.clear(); pending = null;
            call.reject("Device authentication cancelled", "VAULT_CANCELLED");
            return;
        }
        request.executing = true;
        worker.execute(() -> execute(request));
    }

    private void execute(Request request) {
        byte[] value = null;
        JSObject result = new JSObject();
        Exception failure = null;
        try {
            switch (request.operation) {
                case PUT: vault.put(request.record, request.secret); break;
                case GET:
                    value = vault.get(request.record);
                    result.put("secret", new String(value, StandardCharsets.UTF_8));
                    break;
                case REMOVE: vault.remove(request.record); break;
            }
        } catch (Exception e) { failure = e; }
        finally {
            if (value != null) Arrays.fill(value, (byte) 0);
            synchronized (this) { request.clear(); if (pending == request) pending = null; }
        }
        // Release the slot before replying so a caller can immediately start its next operation.
        if (failure == null) request.call.resolve(result);
        else fail(request.call, failure);
    }

    private void fail(PluginCall call, Exception e) {
        String code = "VAULT_STORAGE_FAILED";
        if (e instanceof WalletVaultStore.Failure failure) code = "VAULT_" + failure.code.name();
        else if (e instanceof UserNotAuthenticatedException) code = "VAULT_AUTH_EXPIRED";
        else if (e instanceof KeyPermanentlyInvalidatedException || e instanceof java.security.UnrecoverableKeyException) code = "VAULT_KEY_UNAVAILABLE";
        // No arguments, plaintext, exception messages or arbitrary nested provider data.
        Log.w(TAG, code + " (" + e.getClass().getSimpleName() + ")");
        call.reject("Wallet vault operation failed", code);
    }

    @Override protected synchronized void handleOnDestroy() {
        destroyed = true;
        // Queued/active crypto is allowed to finish; an auth prompt has no disk effects.
        if (pending != null && !worker.isShutdown()) {
            // Cleanup runs after any active work, preventing concurrent buffer erasure.
            Request request = pending;
            worker.execute(request::clear);
            pending = null;
        }
        worker.shutdown();
    }
}
