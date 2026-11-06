package dev.gofastr.nativeui;

import android.view.View;
import org.json.JSONObject;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.function.Consumer;

/** Compiled libraries implement these seams; application bootstrap only registers them. */
public final class Extensions {
    public interface Factory {
        Map<String,String> capabilities(GofastrActivity host);
    }
    public interface ComponentFactory extends Factory {
        Component mount(GofastrActivity host, JSONObject payload, Consumer<String> emit) throws Exception;
    }
    public interface Component {
        View view();
        void update(JSONObject payload, Consumer<String> emit) throws Exception;
        void suspend();
        void dispose();
    }
    public interface FileFactory extends Factory {
        void acquire(GofastrActivity host, JSONObject request, Consumer<JSONObject> result) throws Exception;
    }
    private static final Map<String,Factory> factories = new LinkedHashMap<>();
    public static synchronized void install(String name, Factory factory) { factories.put(name, factory); }
    public static synchronized Factory get(String name) { return factories.get(name); }
    public static synchronized Map<String,String> capabilities(GofastrActivity host) {
        Map<String,String> result = new LinkedHashMap<>();
        for (Factory factory: factories.values()) result.putAll(factory.capabilities(host));
        return result;
    }
}
