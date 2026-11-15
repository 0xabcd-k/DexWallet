package com.dexwallet.app;

import android.content.Context;
import android.content.SharedPreferences;
import com.getcapacitor.JSArray;
import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;
import java.util.ArrayList;

// Unlike Preferences.apply(), commit() reports disk completion and failure.
// Capacitor invokes these methods on its plugin thread, not the UI thread.
@CapacitorPlugin(name = "NativeState")
public class NativeStatePlugin extends Plugin {
    private SharedPreferences preferences;

    @Override
    public void load() {
        preferences = getContext().getSharedPreferences("CapacitorStorage", Context.MODE_PRIVATE);
    }

    @PluginMethod
    public void keys(PluginCall call) {
        JSObject result = new JSObject();
        result.put("keys", new JSArray(new ArrayList<>(preferences.getAll().keySet())));
        call.resolve(result);
    }

    @PluginMethod
    public void get(PluginCall call) {
        String key = call.getString("key");
        if (key == null) { call.reject("Key is required"); return; }
        String value = preferences.getString(key, null);
        JSObject result = new JSObject();
        result.put("value", value == null ? JSObject.NULL : value);
        call.resolve(result);
    }

    @PluginMethod
    public void set(PluginCall call) {
        String key = call.getString("key");
        String value = call.getString("value");
        if (key == null || value == null) { call.reject("Key and value are required"); return; }
        complete(call, preferences.edit().putString(key, value).commit());
    }

    @PluginMethod
    public void remove(PluginCall call) {
        String key = call.getString("key");
        if (key == null) { call.reject("Key is required"); return; }
        complete(call, preferences.edit().remove(key).commit());
    }

    private void complete(PluginCall call, boolean persisted) {
        if (persisted) call.resolve();
        else call.reject("Native state could not be persisted");
    }
}
