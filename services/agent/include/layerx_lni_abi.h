#ifndef LAYERX_LNI_ABI_H
#define LAYERX_LNI_ABI_H

#include <stddef.h>
#include <stdint.h>

#define LAYERX_LNI_ABI_VERSION_MAJOR 1u
#define LAYERX_LNI_ABI_VERSION_MINOR 0u
#define LAYERX_LNI_ABI_VERSION ((LAYERX_LNI_ABI_VERSION_MAJOR << 16u) | LAYERX_LNI_ABI_VERSION_MINOR)

typedef struct layerx_lni_handle layerx_lni_handle;

enum layerx_lni_status {
    LAYERX_LNI_OK = 0,
    LAYERX_LNI_DEADLINE = 1,
    LAYERX_LNI_PEER_SHUTDOWN = 2,
    LAYERX_LNI_CONNECTION_FAILURE = 3,
    LAYERX_LNI_FRAME_VIOLATION = 4
};

uint32_t layerx_lni_abi_version(void);
int32_t layerx_lni_send(layerx_lni_handle *handle, const uint8_t *canonical_frame, size_t frame_len);
int32_t layerx_lni_receive(layerx_lni_handle *handle, const uint8_t **canonical_frame, size_t *frame_len);
void layerx_lni_release(layerx_lni_handle *handle, const uint8_t *canonical_frame, size_t frame_len);
void layerx_lni_close(layerx_lni_handle *handle);

#endif
