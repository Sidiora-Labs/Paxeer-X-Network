#define main budget_lifecycle_main
#include "../daemon/lxp_test_budget_lifecycle.c"
#undef main

static int caps_asset(const uint8_t did[76], const char *salt_path, uint8_t asset[32])
{
    uint8_t issuer[32], salt[32];
    FILE *file = fopen(salt_path, "rb");
    REQUIRE(file != NULL && fread(salt, 1U, 32U, file) == 32U && fgetc(file) == EOF && fclose(file) == 0);
    REQUIRE(lxp_did_id_derive(did, 75U, issuer) == LXP_OK);
    lxp_hash_context hash;
    lxp_hash_init(&hash);
    REQUIRE(lxp_hash_update(&hash, "LX:ASSET:v1", 11U) == LXP_OK && lxp_hash_update(&hash, issuer, 32U) == LXP_OK &&
            lxp_hash_update(&hash, salt, 32U) == LXP_OK && lxp_hash_final(&hash, asset) == LXP_OK);
    return 0;
}

static int caps_budget_account(const uint8_t did[76], const uint8_t object[32], uint8_t budget[32])
{
    char name[256], hex[65];
    for (size_t i = 0U; i < 32U; ++i) (void)snprintf(hex + i * 2U, 3U, "%02x", object[i]);
    int n = snprintf(name, sizeof(name), "agent:%s:budget:%s", did, hex);
    REQUIRE(n > 0 && (size_t)n < sizeof(name));
    REQUIRE(lx_account_id_from_string((const uint8_t *)name, (size_t)n, budget) == LXP_OK);
    return 0;
}

static int caps_grant(const signer *key, const uint8_t from[32], const uint8_t recipient[32],
                      const uint8_t asset[32], uint8_t tag, uint64_t now_ms,
                      uint8_t *payload, size_t capacity, size_t *length)
{
    lxp_payer_grant grant = {0};
    uint8_t message[384];
    size_t message_length;
    memcpy(grant.from, from, 32U);
    memcpy(grant.recipient, recipient, 32U);
    memcpy(grant.asset, asset, 32U);
    memcpy(grant.public_key, key->public_key, 32U);
    memset(grant.purpose_hash, 0x55, 32U);
    grant.reference_hash[0] = tag;
    grant.has_reference = true;
    grant.per_draw_maximum = (lxp_u128){0U, 10U};
    grant.allowance = (lxp_u128){0U, 100U};
    grant.recurring = (tag & 1U) != 0U;
    grant.window_length = grant.recurring ? 30000U : 0U;
    grant.expiration = now_ms + 3600000U;
    grant.revocation_sequence = 1U;
    REQUIRE(lxp_grant_authorization_message(&grant, message, sizeof(message), &message_length) == LXP_OK);
    REQUIRE(lxp_hash_authority(message, message_length, grant.grant_id) == LXP_OK);
    REQUIRE(sign_raw(key, grant.grant_id, 32U, grant.signature) == 0);
    REQUIRE(lxp_payer_grant_encode(&grant, payload, capacity, length) == LXP_OK);
    printf("grant id=");
    print_hex(grant.grant_id, 32U);
    printf(" from=");
    print_hex(from, 32U);
    printf("\n");
    return 0;
}


enum { CAPS_CURSOR = 107, CAPS_REQUEST = 42U, CAPS_RESPONSE = 43U, CAPS_REFUSAL = 25U };

typedef struct caps_context {
    const char *socket_path, *salt_path, *out;
    FILE *frames;
    uint64_t sequence[2], source_sequence[2];
    signer keys[2];
    uint8_t did[2][76], asset[32], account[2][32];
    unsigned activity;
    unsigned passed;
    char cases[4096];
} caps_context;

static int caps_connect(const caps_context *c)
{
    struct sockaddr_un address = {.sun_family = AF_UNIX};
    if (strlen(c->socket_path) >= sizeof(address.sun_path)) return -1;
    strcpy(address.sun_path, c->socket_path);
    int fd = socket(AF_UNIX, SOCK_STREAM, 0);
    if (fd < 0) return -1;
    if (connect(fd, (struct sockaddr *)&address, sizeof(address)) != 0 || handshake(fd) != 0) {
        (void)close(fd);
        return -1;
    }
    return fd;
}

static int caps_case(caps_context *c, const char *name)
{
    size_t used = strlen(c->cases);
    int n = snprintf(c->cases + used, sizeof(c->cases) - used, "%s\"%s\"", used ? "," : "", name);
    REQUIRE(n > 0 && (size_t)n < sizeof(c->cases) - used);
    c->passed++;
    return 0;
}

static int caps_mutate(caps_context *c, unsigned who, const char *operation, uint8_t tag, uint64_t aux, int expected)
{
    uint8_t object[32] = {0x0dU, 0xcaU, 0x95U}, budget[32], recipient[32];
    static uint8_t payload[LXP_MAX_PAYLOAD_BYTES];
    size_t length = 0U;
    uint32_t type;
    struct timespec now;
    char path[PATH_MAX];
    REQUIRE(clock_gettime(CLOCK_REALTIME, &now) == 0);
    uint64_t start = (uint64_t)now.tv_sec * 1000U;
    object[31] = tag;
    memcpy(recipient, c->account[who ^ 1U], 32U);
    REQUIRE(caps_budget_account(c->did[who], object, budget) == 0);
    memset(payload, 0, sizeof(payload));
    payload[1] = 1U;
    memcpy(payload + 2U, object, 32U);
    if (strcmp(operation, "budget-create") == 0) {
        type = LX_BUDGET_CREATE;
        length = LX_BUDGET_CREATE_V2_PAYLOAD_BYTES;
        payload[1] = 2U;
        memcpy(payload + 34U, budget, 32U);
        memcpy(payload + 66U, c->asset, 32U);
        payload[98] = 0x55U;
        REQUIRE(lxp_u128_to_be((lxp_u128){0U, 200U}, payload + 130U) == LXP_OK);
        REQUIRE(lxp_u128_to_be((lxp_u128){0U, 50U}, payload + 146U) == LXP_OK);
        REQUIRE(lxp_u128_to_be((lxp_u128){0U, 100U}, payload + 162U) == LXP_OK);
        store_u64(payload + 178U, 30000U);
        store_u64(payload + 186U, start);
        store_u64(payload + 194U, start + 3600000U);
        store_u64(payload + 202U, 1U);
        payload[210] = LX_BUDGET_ROLLOVER_CAPPED;
        memcpy(payload + 211U, c->account[who], 32U);
        store_u64(payload + 243U, c->source_sequence[who]++);
    } else if (strcmp(operation, "budget-spend") == 0) {
        type = LX_BUDGET_SPEND;
        length = LX_BUDGET_SPEND_PAYLOAD_BYTES;
        memcpy(payload + 34U, recipient, 32U);
        REQUIRE(lxp_u128_to_be((lxp_u128){0U, aux}, payload + 66U) == LXP_OK);
    } else if (strcmp(operation, "budget-revoke") == 0) {
        type = LX_BUDGET_REVOKE;
        length = LX_BUDGET_REVOKE_PAYLOAD_BYTES;
        store_u64(payload + 34U, aux);
    } else if (strcmp(operation, "grant-issue") == 0) {
        type = LX_ASSET_GRANT_ISSUE;
        REQUIRE(caps_grant(&c->keys[who], c->account[who], recipient, c->asset, tag, aux, payload, sizeof(payload), &length) == 0);
    } else if (strcmp(operation, "grant-revoke") == 0) {
        uint8_t grant[LXP_MAX_PAYLOAD_BYTES];
        size_t grant_length;
        lxp_payer_grant issued;
        type = LX_ASSET_GRANT_REVOKE;
        REQUIRE(caps_grant(&c->keys[who], c->account[who], recipient, c->asset, tag, aux, grant, sizeof(grant), &grant_length) == 0);
        REQUIRE(lxp_payer_grant_decode(grant, grant_length, &issued) == LXP_OK);
        length = 42U;
        memcpy(payload + 2U, issued.grant_id, 32U);
        store_u64(payload + 34U, 2U);
    } else {
        return 2;
    }
    REQUIRE(snprintf(path, sizeof(path), "%s/activity-%u.bin", c->out, c->activity++) > 0);
    memcpy(REGISTERED_DID, c->did[who], 76U);
    int fd = caps_connect(c);
    REQUIRE(fd >= 0);
    REQUIRE(budget_submit(fd, &c->keys[who], c->sequence[who]++, type, payload, length, path, false, expected) == 0);
    REQUIRE(close(fd) == 0);
    return 0;
}

static int caps_exchange(caps_context *c, int fd, uint64_t correlation, const uint8_t *request, size_t length,
                         wire_envelope *response)
{
    REQUIRE(send_request(fd, 5U, (uint16_t)CAPS_REQUEST, correlation, request, length) == 0);
    REQUIRE(receive_envelope(fd, response) == 0 && response->correlation_id == correlation);
    uint8_t head[4U + 18U], proof_length[4];
    REQUIRE(response->payload_length <= UINT32_MAX - 22U - response->proof_length);
    store_u32(head, (uint32_t)(22U + response->payload_length + response->proof_length));
    store_u16(head + 4U, response->major);
    store_u16(head + 6U, response->minor);
    store_u16(head + 8U, response->tag);
    store_u64(head + 10U, response->correlation_id);
    store_u32(head + 18U, (uint32_t)response->payload_length);
    store_u32(proof_length, (uint32_t)response->proof_length);
    REQUIRE(fwrite(head, 1U, sizeof(head), c->frames) == sizeof(head));
    REQUIRE(fwrite(response->payload, 1U, response->payload_length, c->frames) == response->payload_length);
    REQUIRE(fwrite(proof_length, 1U, sizeof(proof_length), c->frames) == sizeof(proof_length));
    REQUIRE(fwrite(response->proof, 1U, response->proof_length, c->frames) == response->proof_length);
    return 0;
}

static int caps_refused(caps_context *c, int fd, uint64_t correlation, const uint8_t *request, size_t length, int32_t code)
{
    wire_envelope response;
    REQUIRE(caps_exchange(c, fd, correlation, request, length, &response) == 0);
    bool ok = response.tag == CAPS_REFUSAL && response.payload_length == 5U && (int32_t)load_u32(response.payload + 1U) == code;
    if (!ok) fprintf(stderr, "caps expected refusal %d got tag=%u length=%zu\n", code, response.tag, (size_t)response.payload_length);
    release_envelope(&response);
    REQUIRE(ok);
    return 0;
}

static size_t caps_open_request(uint8_t request[43], const uint8_t selector[32], uint16_t items, uint32_t bytes)
{
    store_u16(request, 1U);
    request[2] = 1U;
    store_u32(request + 3U, NETWORK_ID);
    memcpy(request + 7U, selector, 32U);
    store_u16(request + 39U, items);
    store_u32(request + 39U + 2U, bytes);
    return 43U;
}

static size_t caps_page_request(uint8_t request[116], const uint8_t cursor[CAPS_CURSOR], uint16_t items, uint32_t bytes)
{
    store_u16(request, 1U);
    request[2] = 2U;
    memcpy(request + 3U, cursor, CAPS_CURSOR);
    store_u16(request + 110U, items);
    store_u32(request + 112U, bytes);
    return 116U;
}

static int caps_skip_bound(const uint8_t *p, size_t length, size_t *offset)
{
    REQUIRE(*offset < length);
    uint8_t flag = p[(*offset)++];
    if (flag == 2U) {
        REQUIRE(length - *offset >= 4U);
        size_t w = load_u32(p + *offset);
        REQUIRE(length - *offset - 4U >= w);
        *offset += 4U + w;
    } else if (flag == 3U) {
        REQUIRE(length - *offset >= 7U);
        size_t n = p[*offset + 6U];
        REQUIRE(length - *offset - 7U >= n * 32U);
        *offset += 7U + n * 32U;
    } else {
        REQUIRE(flag <= 1U);
    }
    return 0;
}

/* Parses an op=2 page; returns item count, exhausted flag and next cursor. */
static int caps_parse_page(const wire_envelope *r, const uint8_t cursor[CAPS_CURSOR], unsigned *items,
                           bool *exhausted, uint8_t next[CAPS_CURSOR])
{
    const uint8_t *p = r->payload;
    size_t length = r->payload_length, offset = 3U + CAPS_CURSOR;
    REQUIRE(r->tag == CAPS_RESPONSE && r->proof_length == 96U && length >= offset + 2U);
    REQUIRE(load_u16(p) == 1U && p[2] == 2U && memcmp(p + 3U, cursor, CAPS_CURSOR) == 0);
    *items = load_u16(p + offset);
    offset += 2U;
    for (unsigned i = 0U; i < *items; ++i) {
        REQUIRE(length - offset >= 4U);
        size_t w = load_u32(p + offset);
        lxp_state_witness *witness = calloc(1U, sizeof(*witness));
        REQUIRE(witness != NULL && length - offset - 4U >= w);
        REQUIRE(lxp_state_proof_decode(p + offset + 4U, w, witness) == LXP_OK);
        free(witness);
        offset += 4U + w;
    }
    REQUIRE(caps_skip_bound(p, length, &offset) == 0 && caps_skip_bound(p, length, &offset) == 0);
    REQUIRE(offset < length);
    *exhausted = p[offset++] == 1U;
    if (*exhausted) {
        REQUIRE(offset == length);
    } else {
        REQUIRE(length - offset == CAPS_CURSOR);
        memcpy(next, p + offset, CAPS_CURSOR);
    }
    return 0;
}

/* Opens a snapshot for the selector and pages it to exhaustion, recording every frame. */
static int caps_traverse(caps_context *c, int fd, const uint8_t selector[32], uint8_t root[32], unsigned *total,
                         bool mutate_midway)
{
    uint8_t request[116], cursor[CAPS_CURSOR], next[CAPS_CURSOR], previous[CAPS_CURSOR];
    wire_envelope response;
    uint64_t correlation = 1U;
    size_t length = caps_open_request(request, selector, 2U, 65536U);
    REQUIRE(caps_exchange(c, fd, correlation++, request, length, &response) == 0);
    if (response.tag != CAPS_RESPONSE) fprintf(stderr, "caps open refused tag=%u\n", response.tag);
    REQUIRE(response.tag == CAPS_RESPONSE && response.proof_length == 96U && response.payload_length > 3U + 76U + CAPS_CURSOR);
    REQUIRE(load_u16(response.payload) == 1U && response.payload[2] == 1U);
    REQUIRE(load_u32(response.payload + 35U) == NETWORK_ID);
    memcpy(root, response.payload + 39U, 32U);
    memcpy(cursor, response.payload + response.payload_length - CAPS_CURSOR, CAPS_CURSOR);
    REQUIRE(load_u16(cursor) == 1U && memcmp(cursor + 38U, root, 32U) == 0 && cursor[102] == 1U);
    release_envelope(&response);
    *total = 0U;
    bool replayed = false, exhausted = false;
    while (!exhausted) {
        unsigned items;
        length = caps_page_request(request, cursor, 2U, 65536U);
        REQUIRE(caps_exchange(c, fd, correlation++, request, length, &response) == 0);
        REQUIRE(caps_parse_page(&response, cursor, &items, &exhausted, next) == 0);
        release_envelope(&response);
        *total += items;
        if (!exhausted) {
            REQUIRE(memcmp(next + 38U, root, 32U) == 0);
            REQUIRE(next[102] > cursor[102] || load_u32(next + 103U) > load_u32(cursor + 103U));
            memcpy(previous, cursor, CAPS_CURSOR);
            memcpy(cursor, next, CAPS_CURSOR);
            if (!replayed) {
                length = caps_page_request(request, previous, 2U, 65536U);
                REQUIRE(caps_refused(c, fd, correlation++, request, length, LXP_ERR_MALFORMED_ENVELOPE) == 0);
                REQUIRE(caps_case(c, "native_cursor_replay_refused") == 0);
                replayed = true;
            }
            if (mutate_midway) {
                REQUIRE(caps_mutate(c, 0U, "budget-spend", 1U, 5U, 0) == 0);
                REQUIRE(caps_mutate(c, 0U, "grant-revoke", 1U, strtoull(getenv("CAPS_GRANT_ISSUED_MS"), NULL, 10), 0) == 0);
                mutate_midway = false;
            }
        }
    }
    return 0;
}

static int caps_native_negatives(caps_context *c, int fd, const uint8_t selector[32])
{
    uint8_t request[116], cursor[CAPS_CURSOR], other[CAPS_CURSOR];
    wire_envelope response;
    size_t length = caps_open_request(request, selector, 65U, 65536U);
    REQUIRE(caps_refused(c, fd, 100U, request, length, LXP_ERR_LENGTH_LIMIT) == 0);
    REQUIRE(caps_case(c, "native_oversize_page_refused") == 0);
    length = caps_open_request(request, selector, 1U, 65536U);
    REQUIRE(caps_exchange(c, fd, 101U, request, length, &response) == 0 && response.tag == CAPS_RESPONSE);
    memcpy(cursor, response.payload + response.payload_length - CAPS_CURSOR, CAPS_CURSOR);
    release_envelope(&response);
    memcpy(other, cursor, CAPS_CURSOR);
    other[70] ^= 1U;
    length = caps_page_request(request, other, 1U, 65536U);
    REQUIRE(caps_refused(c, fd, 102U, request, length, LXP_ERR_MALFORMED_ENVELOPE) == 0);
    REQUIRE(caps_case(c, "native_wrong_selection_refused") == 0);
    memcpy(other, cursor, CAPS_CURSOR);
    store_u32(other + 103U, load_u32(cursor + 103U) + 1U);
    length = caps_page_request(request, other, 1U, 65536U);
    REQUIRE(caps_refused(c, fd, 103U, request, length, LXP_ERR_MALFORMED_ENVELOPE) == 0);
    REQUIRE(caps_case(c, "native_cursor_gap_refused") == 0);
    memcpy(other, cursor, CAPS_CURSOR);
    other[40] ^= 1U;
    length = caps_page_request(request, other, 1U, 65536U);
    REQUIRE(caps_refused(c, fd, 104U, request, length, LXP_ERR_MALFORMED_ENVELOPE) == 0);
    REQUIRE(caps_case(c, "native_cross_root_cursor_refused") == 0);
    length = caps_page_request(request, cursor, 0U, 65536U);
    REQUIRE(caps_refused(c, fd, 105U, request, length, LXP_ERR_MALFORMED_ENVELOPE) == 0);
    REQUIRE(caps_case(c, "native_no_progress_refused") == 0);
    length = caps_page_request(request, cursor, 1U, 65536U);
    REQUIRE(caps_refused(c, fd, 106U, request, length - 1U, LXP_ERR_MALFORMED_ENVELOPE) == 0);
    REQUIRE(caps_case(c, "native_malformed_cursor_refused") == 0);
    int foreign = caps_connect(c);
    REQUIRE(foreign >= 0);
    REQUIRE(caps_refused(c, foreign, 107U, request, length, LXP_ERR_EXPIRED) == 0);
    REQUIRE(close(foreign) == 0);
    REQUIRE(caps_case(c, "native_foreign_connection_cursor_refused") == 0);
    for (unsigned i = 0U; i < 4U; ++i) {
        length = caps_open_request(request, selector, 1U, 65536U);
        REQUIRE(caps_exchange(c, fd, 110U + i, request, length, &response) == 0);
        bool accepted = response.tag == CAPS_RESPONSE;
        release_envelope(&response);
        if (!accepted) break;
    }
    length = caps_open_request(request, selector, 1U, 65536U);
    REQUIRE(caps_refused(c, fd, 120U, request, length, LXP_ERR_LENGTH_LIMIT) == 0);
    REQUIRE(caps_case(c, "native_active_snapshot_limit_refused") == 0);
    uint8_t release[35];
    store_u16(release, 1U);
    release[2] = 3U;
    memcpy(release + 3U, cursor + 2U, 32U);
    REQUIRE(caps_exchange(c, fd, 121U, release, sizeof(release), &response) == 0);
    REQUIRE(response.tag == CAPS_RESPONSE && response.payload_length == 35U && memcmp(response.payload + 3U, cursor + 2U, 32U) == 0);
    release_envelope(&response);
    length = caps_page_request(request, cursor, 1U, 65536U);
    REQUIRE(caps_refused(c, fd, 122U, request, length, LXP_ERR_EXPIRED) == 0);
    REQUIRE(caps_case(c, "native_released_snapshot_refused") == 0);
    FILE *file;
    char path[PATH_MAX];
    REQUIRE(snprintf(path, sizeof(path), "%s/disconnect-cursor.bin", c->out) > 0);
    length = caps_open_request(request, selector, 1U, 65536U);
    int dropped = caps_connect(c);
    REQUIRE(dropped >= 0 && caps_exchange(c, dropped, 130U, request, length, &response) == 0 && response.tag == CAPS_RESPONSE);
    file = fopen(path, "wx");
    REQUIRE(file != NULL && fwrite(response.payload + response.payload_length - CAPS_CURSOR, 1U, CAPS_CURSOR, file) == CAPS_CURSOR && fclose(file) == 0);
    release_envelope(&response);
    REQUIRE(close(dropped) == 0);
    return 0;
}

static int caps_usage(void)
{
    fprintf(stderr, "usage: lxp_wallet_caps_discovery --out <dir> --scenario <populated|mixed-owner|prefix-empty|empty-module|mutation>\n");
    return 2;
}

static int caps_env_u64(const char *name, uint64_t *value)
{
    const char *text = getenv(name);
    char *end = NULL;
    if (text == NULL || *text == 0) return 1;
    *value = strtoull(text, &end, 10);
    return end == NULL || *end != 0;
}

int caps_discovery_main(int argc, char **argv)
{
    static caps_context c;
    const char *scenario = NULL;
    for (int i = 1; i + 1 < argc; i += 2) {
        if (strcmp(argv[i], "--out") == 0) c.out = argv[i + 1];
        else if (strcmp(argv[i], "--scenario") == 0) scenario = argv[i + 1];
        else return caps_usage();
    }
    if (argc != 5 || c.out == NULL || scenario == NULL) return caps_usage();
    c.socket_path = getenv("LAYERX_CAPS_SOCKET");
    c.salt_path = getenv("LAYERX_CAPS_SALT");
    if (c.socket_path == NULL || c.salt_path == NULL || getenv("PAXEER_X_FIXTURE_KEYS") == NULL ||
        caps_env_u64("CAPS_TREASURY_SEQUENCE", &c.sequence[0]) || caps_env_u64("CAPS_BOB_SEQUENCE", &c.sequence[1]) ||
        caps_env_u64("CAPS_TREASURY_SOURCE_SEQUENCE", &c.source_sequence[0]) ||
        caps_env_u64("CAPS_BOB_SOURCE_SEQUENCE", &c.source_sequence[1])) {
        fprintf(stderr, "caps fixture prerequisite missing: socket, salt, fixture keys or account sequences\n");
        return 3;
    }
    REQUIRE(fixture_signer(&c.keys[0], "treasury") == 0 && fixture_signer(&c.keys[1], "bob") == 0);
    actor_did(&c.keys[0], c.did[0]);
    actor_did(&c.keys[1], c.did[1]);
    REQUIRE(caps_asset(c.did[0], c.salt_path, c.asset) == 0);
    REQUIRE(account_id(c.did[0], c.asset, c.account[0]) == 0 && account_id(c.did[1], c.asset, c.account[1]) == 0);
    char path[PATH_MAX];
    REQUIRE(snprintf(path, sizeof(path), "%s/%s.bin", c.out, scenario) > 0);
    c.frames = fopen(path, "wx");
    REQUIRE(c.frames != NULL);
    struct timespec now;
    REQUIRE(clock_gettime(CLOCK_REALTIME, &now) == 0);
    uint64_t issued = (uint64_t)now.tv_sec * 1000U;
    uint8_t root[32], fresh[32];
    unsigned total = 0U, fresh_total = 0U;
    if (strcmp(scenario, "empty-module") == 0) {
        /* Budget module has no records yet: the budget prefix is proven by the canonical empty module root. */
    } else if (strcmp(scenario, "prefix-empty") == 0) {
        REQUIRE(caps_mutate(&c, 0U, "budget-create", 1U, 0U, 0) == 0);
    } else if (strcmp(scenario, "populated") == 0) {
        REQUIRE(caps_mutate(&c, 0U, "budget-create", 2U, 0U, 0) == 0);
        REQUIRE(caps_mutate(&c, 0U, "grant-issue", 1U, issued, 0) == 0);
        REQUIRE(caps_mutate(&c, 0U, "grant-issue", 2U, issued, 0) == 0);
    } else if (strcmp(scenario, "mixed-owner") == 0) {
        REQUIRE(caps_mutate(&c, 1U, "budget-create", 3U, 0U, 0) == 0);
        REQUIRE(caps_mutate(&c, 1U, "grant-issue", 3U, issued, 0) == 0);
        REQUIRE(caps_mutate(&c, 0U, "budget-create", 4U, 0U, 0) == 0);
    } else if (strcmp(scenario, "mutation") != 0) {
        return caps_usage();
    }
    int fd = caps_connect(&c);
    REQUIRE(fd >= 0);
    bool mutation = strcmp(scenario, "mutation") == 0;
    if (mutation) {
        uint64_t grant_issued;
        if (caps_env_u64("CAPS_GRANT_ISSUED_MS", &grant_issued)) {
            fprintf(stderr, "caps fixture prerequisite missing: CAPS_GRANT_ISSUED_MS from the populated scenario\n");
            return 3;
        }
    }
    REQUIRE(caps_traverse(&c, fd, c.account[0], root, &total, mutation) == 0);
    REQUIRE(caps_case(&c, "native_complete_traversal") == 0);
    if (mutation) {
        REQUIRE(caps_traverse(&c, fd, c.account[0], fresh, &fresh_total, false) == 0);
        REQUIRE(memcmp(root, fresh, 32U) != 0);
        REQUIRE(caps_case(&c, "native_mutation_during_traversal_kept_snapshot_root") == 0);
        REQUIRE(caps_case(&c, "native_fresh_snapshot_after_mutation") == 0);
        REQUIRE(caps_native_negatives(&c, fd, c.account[0]) == 0);
    }
    REQUIRE(close(fd) == 0 && fclose(c.frames) == 0);
    REQUIRE(snprintf(path, sizeof(path), "%s/%s.json", c.out, scenario) > 0);
    FILE *json = fopen(path, "wx");
    REQUIRE(json != NULL);
    fprintf(json, "{\"scenario\":\"%s\",\"network_id\":%u,\"state_root\":\"", scenario, (unsigned)NETWORK_ID);
    for (size_t i = 0U; i < 32U; ++i) fprintf(json, "%02x", root[i]);
    fprintf(json, "\",\"selector_account\":\"");
    for (size_t i = 0U; i < 32U; ++i) fprintf(json, "%02x", c.account[0][i]);
    fprintf(json, "\",\"foreign_account\":\"");
    for (size_t i = 0U; i < 32U; ++i) fprintf(json, "%02x", c.account[1][i]);
    fprintf(json, "\",\"items\":%u,\"grant_issued_ms\":%llu,\"next_sequence\":{\"treasury\":%llu,\"bob\":%llu},"
                  "\"next_source_sequence\":{\"treasury\":%llu,\"bob\":%llu},\"cases\":[%s]}\n",
            total, (unsigned long long)issued, (unsigned long long)c.sequence[0], (unsigned long long)c.sequence[1],
            (unsigned long long)c.source_sequence[0], (unsigned long long)c.source_sequence[1], c.cases);
    REQUIRE(fclose(json) == 0);
    printf("caps scenario=%s items=%u cases=%u\n", scenario, total, c.passed);
    return 0;
}
