package com.sidiora.layerx.spring;

import java.io.IOException;

@FunctionalInterface
public interface ResourceRelease {
    LayerXResource release() throws IOException;
}
