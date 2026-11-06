package dev.gofastr.nativeui;

import org.json.JSONObject;
import java.util.Collections;
import java.util.Map;
import java.util.function.Consumer;

/** The media extension owns its compiled adapter and emits opaque event keys. */
public final class GofastrMediaComponent implements Extensions.ComponentFactory {
    @Override public Map<String,String> capabilities(GofastrActivity host){return Collections.singletonMap("gofastr.media/v1","available");}
    @Override public Extensions.Component mount(GofastrActivity host,JSONObject payload,Consumer<String> emit) throws Exception {
        return new MediaContent(host,payload,emit);
    }
}
