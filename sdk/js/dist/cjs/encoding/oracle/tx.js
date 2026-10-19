"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.MsgDelegateFeedConsentResponse = exports.MsgDelegateFeedConsent = exports.MsgAggregateExchangeRateVoteResponse = exports.MsgAggregateExchangeRateVote = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.MsgAggregateExchangeRateVote = {
    $type: "paxprotocol.paxchain.oracle.MsgAggregateExchangeRateVote",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.exchange_rates !== "") {
            writer.uint32(18).string(message.exchange_rates);
        }
        if (message.feeder !== "") {
            writer.uint32(26).string(message.feeder);
        }
        if (message.validator !== "") {
            writer.uint32(34).string(message.validator);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgAggregateExchangeRateVote();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.exchange_rates = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.feeder = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.validator = reader.string();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            exchange_rates: isSet(object.exchange_rates) ? globalThis.String(object.exchange_rates) : "",
            feeder: isSet(object.feeder) ? globalThis.String(object.feeder) : "",
            validator: isSet(object.validator) ? globalThis.String(object.validator) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.exchange_rates !== "") {
            obj.exchange_rates = message.exchange_rates;
        }
        if (message.feeder !== "") {
            obj.feeder = message.feeder;
        }
        if (message.validator !== "") {
            obj.validator = message.validator;
        }
        return obj;
    },
    create(base) {
        return exports.MsgAggregateExchangeRateVote.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgAggregateExchangeRateVote();
        message.exchange_rates = object.exchange_rates ?? "";
        message.feeder = object.feeder ?? "";
        message.validator = object.validator ?? "";
        return message;
    }
};
exports.MsgAggregateExchangeRateVoteResponse = {
    $type: "paxprotocol.paxchain.oracle.MsgAggregateExchangeRateVoteResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgAggregateExchangeRateVoteResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(_) {
        return {};
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    create(base) {
        return exports.MsgAggregateExchangeRateVoteResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgAggregateExchangeRateVoteResponse();
        return message;
    }
};
exports.MsgDelegateFeedConsent = {
    $type: "paxprotocol.paxchain.oracle.MsgDelegateFeedConsent",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.operator !== "") {
            writer.uint32(10).string(message.operator);
        }
        if (message.delegate !== "") {
            writer.uint32(18).string(message.delegate);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgDelegateFeedConsent();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.operator = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.delegate = reader.string();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            operator: isSet(object.operator) ? globalThis.String(object.operator) : "",
            delegate: isSet(object.delegate) ? globalThis.String(object.delegate) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.operator !== "") {
            obj.operator = message.operator;
        }
        if (message.delegate !== "") {
            obj.delegate = message.delegate;
        }
        return obj;
    },
    create(base) {
        return exports.MsgDelegateFeedConsent.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgDelegateFeedConsent();
        message.operator = object.operator ?? "";
        message.delegate = object.delegate ?? "";
        return message;
    }
};
exports.MsgDelegateFeedConsentResponse = {
    $type: "paxprotocol.paxchain.oracle.MsgDelegateFeedConsentResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgDelegateFeedConsentResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(_) {
        return {};
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    create(base) {
        return exports.MsgDelegateFeedConsentResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgDelegateFeedConsentResponse();
        return message;
    }
};
function createBaseMsgAggregateExchangeRateVote() {
    return { exchange_rates: "", feeder: "", validator: "" };
}
function createBaseMsgAggregateExchangeRateVoteResponse() {
    return {};
}
function createBaseMsgDelegateFeedConsent() {
    return { operator: "", delegate: "" };
}
function createBaseMsgDelegateFeedConsentResponse() {
    return {};
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/paxprotocol.paxchain.oracle.MsgAggregateExchangeRateVote", exports.MsgAggregateExchangeRateVote],
    ["/paxprotocol.paxchain.oracle.MsgAggregateExchangeRateVoteResponse", exports.MsgAggregateExchangeRateVoteResponse],
    ["/paxprotocol.paxchain.oracle.MsgDelegateFeedConsent", exports.MsgDelegateFeedConsent],
    ["/paxprotocol.paxchain.oracle.MsgDelegateFeedConsentResponse", exports.MsgDelegateFeedConsentResponse]
];
exports.aminoConverters = {
    "/paxprotocol.paxchain.oracle.MsgAggregateExchangeRateVote": {
        aminoType: "oracle/MsgAggregateExchangeRateVote",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.MsgAggregateExchangeRateVoteResponse": {
        aminoType: "oracle/MsgAggregateExchangeRateVoteResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.MsgDelegateFeedConsent": {
        aminoType: "oracle/MsgDelegateFeedConsent",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.MsgDelegateFeedConsentResponse": {
        aminoType: "oracle/MsgDelegateFeedConsentResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
