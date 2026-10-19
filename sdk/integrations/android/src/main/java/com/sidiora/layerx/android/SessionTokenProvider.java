package com.sidiora.layerx.android;

/** Runtime-only credential source: the binding accepts no statically configured secret. */
public interface SessionTokenProvider {
    EphemeralSessionToken token();
    void invalidate();
}
