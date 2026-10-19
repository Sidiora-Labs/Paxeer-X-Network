"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.pointerTypeFromJSON = pointerTypeFromJSON;
exports.pointerTypeToJSON = pointerTypeToJSON;
exports.assetTypeFromJSON = assetTypeFromJSON;
exports.assetTypeToJSON = assetTypeToJSON;
const evm_1 = require("../../types/evm");
function pointerTypeFromJSON(object) {
    switch (object) {
        case 0:
        case "ERC20":
            return evm_1.PointerType.ERC20;
        case 1:
        case "ERC721":
            return evm_1.PointerType.ERC721;
        case 2:
        case "NATIVE":
            return evm_1.PointerType.NATIVE;
        case 3:
        case "CW20":
            return evm_1.PointerType.CW20;
        case 4:
        case "CW721":
            return evm_1.PointerType.CW721;
        case 5:
        case "ERC1155":
            return evm_1.PointerType.ERC1155;
        case 6:
        case "CW1155":
            return evm_1.PointerType.CW1155;
        case -1:
        case "UNRECOGNIZED":
        default:
            return evm_1.PointerType.UNRECOGNIZED;
    }
}
function pointerTypeToJSON(object) {
    switch (object) {
        case evm_1.PointerType.ERC20:
            return "ERC20";
        case evm_1.PointerType.ERC721:
            return "ERC721";
        case evm_1.PointerType.NATIVE:
            return "NATIVE";
        case evm_1.PointerType.CW20:
            return "CW20";
        case evm_1.PointerType.CW721:
            return "CW721";
        case evm_1.PointerType.ERC1155:
            return "ERC1155";
        case evm_1.PointerType.CW1155:
            return "CW1155";
        case evm_1.PointerType.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function assetTypeFromJSON(object) {
    switch (object) {
        case 0:
        case "TYPECW20":
            return evm_1.AssetType.TYPECW20;
        case 1:
        case "TYPECW721":
            return evm_1.AssetType.TYPECW721;
        case -1:
        case "UNRECOGNIZED":
        default:
            return evm_1.AssetType.UNRECOGNIZED;
    }
}
function assetTypeToJSON(object) {
    switch (object) {
        case evm_1.AssetType.TYPECW20:
            return "TYPECW20";
        case evm_1.AssetType.TYPECW721:
            return "TYPECW721";
        case evm_1.AssetType.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
