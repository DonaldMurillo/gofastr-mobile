package dev.gofastr.nativeui;

import org.json.JSONArray;
import org.json.JSONObject;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.Map;

/** Internal wire helpers. Applications author typed Go definitions. */
public final class JSON {
    private JSON() {}
    public static JSONObject object(Object... pairs) {
        JSONObject out = new JSONObject();
        try { for (int i=0; i<pairs.length; i+=2) out.put((String)pairs[i], pairs[i+1]); }
        catch (Exception e) { throw new IllegalArgumentException("Invalid wire value", e); }
        return out;
    }
    public static JSONArray array(JSONObject object, String key) {
        JSONArray array = object == null ? null : object.optJSONArray(key);
        return array == null ? new JSONArray() : array;
    }
    public static JSONObject copy(JSONObject value) {
        try { return value == null ? new JSONObject() : new JSONObject(value.toString()); }
        catch (Exception e) { throw new IllegalArgumentException(e); }
    }
    public static Map<String,String> values(JSONObject value) {
        Map<String,String> result = new LinkedHashMap<>();
        if (value != null) { Iterator<String> keys=value.keys(); while(keys.hasNext()) { String k=keys.next(); result.put(k,value.optString(k)); } }
        return result;
    }
    public static JSONObject values(Map<String,String> values) { return new JSONObject(values); }
}
