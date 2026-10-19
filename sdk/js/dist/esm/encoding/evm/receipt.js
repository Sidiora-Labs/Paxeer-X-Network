import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
export const Log = {
    $type: "paxprotocol.paxchain.evm.Log",
    encode(message, writer = new BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        for (const v of message.topics) {
            writer.uint32(18).string(v);
        }
        if (message.data.length !== 0) {
            writer.uint32(26).bytes(message.data);
        }
        if (message.index !== 0) {
            writer.uint32(32).uint32(message.index);
        }
        if (message.synthetic !== false) {
            writer.uint32(40).bool(message.synthetic);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseLog();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.topics.push(reader.string());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.data = reader.bytes();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.index = reader.uint32();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.synthetic = reader.bool();
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
            address: isSet(object.address) ? globalThis.String(object.address) : "",
            topics: globalThis.Array.isArray(object?.topics) ? object.topics.map((e) => globalThis.String(e)) : [],
            data: isSet(object.data) ? bytesFromBase64(object.data) : new Uint8Array(0),
            index: isSet(object.index) ? globalThis.Number(object.index) : 0,
            synthetic: isSet(object.synthetic) ? globalThis.Boolean(object.synthetic) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.topics?.length) {
            obj.topics = message.topics;
        }
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        if (message.index !== 0) {
            obj.index = Math.round(message.index);
        }
        if (message.synthetic !== false) {
            obj.synthetic = message.synthetic;
        }
        return obj;
    },
    create(base) {
        return Log.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseLog();
        message.address = object.address ?? "";
        message.topics = object.topics?.map((e) => e) || [];
        message.data = object.data ?? new Uint8Array(0);
        message.index = object.index ?? 0;
        message.synthetic = object.synthetic ?? false;
        return message;
    }
};
export const Receipt = {
    $type: "paxprotocol.paxchain.evm.Receipt",
    encode(message, writer = new BinaryWriter()) {
        if (message.tx_type !== 0) {
            writer.uint32(8).uint32(message.tx_type);
        }
        if (message.cumulative_gas_used !== 0) {
            writer.uint32(16).uint64(message.cumulative_gas_used);
        }
        if (message.contract_address !== "") {
            writer.uint32(26).string(message.contract_address);
        }
        if (message.tx_hash_hex !== "") {
            writer.uint32(34).string(message.tx_hash_hex);
        }
        if (message.gas_used !== 0) {
            writer.uint32(40).uint64(message.gas_used);
        }
        if (message.effective_gas_price !== 0) {
            writer.uint32(48).uint64(message.effective_gas_price);
        }
        if (message.block_number !== 0) {
            writer.uint32(56).uint64(message.block_number);
        }
        if (message.transaction_index !== 0) {
            writer.uint32(64).uint32(message.transaction_index);
        }
        if (message.status !== 0) {
            writer.uint32(72).uint32(message.status);
        }
        if (message.from !== "") {
            writer.uint32(82).string(message.from);
        }
        if (message.to !== "") {
            writer.uint32(90).string(message.to);
        }
        if (message.vm_error !== "") {
            writer.uint32(98).string(message.vm_error);
        }
        for (const v of message.logs) {
            Log.encode(v, writer.uint32(106).fork()).join();
        }
        if (message.logsBloom.length !== 0) {
            writer.uint32(114).bytes(message.logsBloom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseReceipt();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.tx_type = reader.uint32();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.cumulative_gas_used = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.contract_address = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.tx_hash_hex = reader.string();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.gas_used = longToNumber(reader.uint64());
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.effective_gas_price = longToNumber(reader.uint64());
                    continue;
                case 7:
                    if (tag !== 56) {
                        break;
                    }
                    message.block_number = longToNumber(reader.uint64());
                    continue;
                case 8:
                    if (tag !== 64) {
                        break;
                    }
                    message.transaction_index = reader.uint32();
                    continue;
                case 9:
                    if (tag !== 72) {
                        break;
                    }
                    message.status = reader.uint32();
                    continue;
                case 10:
                    if (tag !== 82) {
                        break;
                    }
                    message.from = reader.string();
                    continue;
                case 11:
                    if (tag !== 90) {
                        break;
                    }
                    message.to = reader.string();
                    continue;
                case 12:
                    if (tag !== 98) {
                        break;
                    }
                    message.vm_error = reader.string();
                    continue;
                case 13:
                    if (tag !== 106) {
                        break;
                    }
                    message.logs.push(Log.decode(reader, reader.uint32()));
                    continue;
                case 14:
                    if (tag !== 114) {
                        break;
                    }
                    message.logsBloom = reader.bytes();
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
            tx_type: isSet(object.tx_type) ? globalThis.Number(object.tx_type) : 0,
            cumulative_gas_used: isSet(object.cumulative_gas_used) ? globalThis.Number(object.cumulative_gas_used) : 0,
            contract_address: isSet(object.contract_address) ? globalThis.String(object.contract_address) : "",
            tx_hash_hex: isSet(object.tx_hash_hex) ? globalThis.String(object.tx_hash_hex) : "",
            gas_used: isSet(object.gas_used) ? globalThis.Number(object.gas_used) : 0,
            effective_gas_price: isSet(object.effective_gas_price) ? globalThis.Number(object.effective_gas_price) : 0,
            block_number: isSet(object.block_number) ? globalThis.Number(object.block_number) : 0,
            transaction_index: isSet(object.transaction_index) ? globalThis.Number(object.transaction_index) : 0,
            status: isSet(object.status) ? globalThis.Number(object.status) : 0,
            from: isSet(object.from) ? globalThis.String(object.from) : "",
            to: isSet(object.to) ? globalThis.String(object.to) : "",
            vm_error: isSet(object.vm_error) ? globalThis.String(object.vm_error) : "",
            logs: globalThis.Array.isArray(object?.logs) ? object.logs.map((e) => Log.fromJSON(e)) : [],
            logsBloom: isSet(object.logsBloom) ? bytesFromBase64(object.logsBloom) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.tx_type !== 0) {
            obj.tx_type = Math.round(message.tx_type);
        }
        if (message.cumulative_gas_used !== 0) {
            obj.cumulative_gas_used = Math.round(message.cumulative_gas_used);
        }
        if (message.contract_address !== "") {
            obj.contract_address = message.contract_address;
        }
        if (message.tx_hash_hex !== "") {
            obj.tx_hash_hex = message.tx_hash_hex;
        }
        if (message.gas_used !== 0) {
            obj.gas_used = Math.round(message.gas_used);
        }
        if (message.effective_gas_price !== 0) {
            obj.effective_gas_price = Math.round(message.effective_gas_price);
        }
        if (message.block_number !== 0) {
            obj.block_number = Math.round(message.block_number);
        }
        if (message.transaction_index !== 0) {
            obj.transaction_index = Math.round(message.transaction_index);
        }
        if (message.status !== 0) {
            obj.status = Math.round(message.status);
        }
        if (message.from !== "") {
            obj.from = message.from;
        }
        if (message.to !== "") {
            obj.to = message.to;
        }
        if (message.vm_error !== "") {
            obj.vm_error = message.vm_error;
        }
        if (message.logs?.length) {
            obj.logs = message.logs.map((e) => Log.toJSON(e));
        }
        if (message.logsBloom.length !== 0) {
            obj.logsBloom = base64FromBytes(message.logsBloom);
        }
        return obj;
    },
    create(base) {
        return Receipt.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseReceipt();
        message.tx_type = object.tx_type ?? 0;
        message.cumulative_gas_used = object.cumulative_gas_used ?? 0;
        message.contract_address = object.contract_address ?? "";
        message.tx_hash_hex = object.tx_hash_hex ?? "";
        message.gas_used = object.gas_used ?? 0;
        message.effective_gas_price = object.effective_gas_price ?? 0;
        message.block_number = object.block_number ?? 0;
        message.transaction_index = object.transaction_index ?? 0;
        message.status = object.status ?? 0;
        message.from = object.from ?? "";
        message.to = object.to ?? "";
        message.vm_error = object.vm_error ?? "";
        message.logs = object.logs?.map((e) => Log.fromPartial(e)) || [];
        message.logsBloom = object.logsBloom ?? new Uint8Array(0);
        return message;
    }
};
function createBaseLog() {
    return { address: "", topics: [], data: new Uint8Array(0), index: 0, synthetic: false };
}
function createBaseReceipt() {
    return {
        tx_type: 0,
        cumulative_gas_used: 0,
        contract_address: "",
        tx_hash_hex: "",
        gas_used: 0,
        effective_gas_price: 0,
        block_number: 0,
        transaction_index: 0,
        status: 0,
        from: "",
        to: "",
        vm_error: "",
        logs: [],
        logsBloom: new Uint8Array(0)
    };
}
function bytesFromBase64(b64) {
    if (globalThis.Buffer) {
        return Uint8Array.from(globalThis.Buffer.from(b64, "base64"));
    }
    else {
        const bin = globalThis.atob(b64);
        const arr = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; ++i) {
            arr[i] = bin.charCodeAt(i);
        }
        return arr;
    }
}
function base64FromBytes(arr) {
    if (globalThis.Buffer) {
        return globalThis.Buffer.from(arr).toString("base64");
    }
    else {
        const bin = [];
        arr.forEach((byte) => {
            bin.push(globalThis.String.fromCharCode(byte));
        });
        return globalThis.btoa(bin.join(""));
    }
}
function longToNumber(int64) {
    const num = globalThis.Number(int64.toString());
    if (num > globalThis.Number.MAX_SAFE_INTEGER) {
        throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
    }
    if (num < globalThis.Number.MIN_SAFE_INTEGER) {
        throw new globalThis.Error("Value is smaller than Number.MIN_SAFE_INTEGER");
    }
    return num;
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/paxprotocol.paxchain.evm.Log", Log],
    ["/paxprotocol.paxchain.evm.Receipt", Receipt]
];
export const aminoConverters = {
    "/paxprotocol.paxchain.evm.Log": {
        aminoType: "evm/Log",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.Receipt": {
        aminoType: "evm/Receipt",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
