export var PointerType;
(function (PointerType) {
    PointerType[PointerType["ERC20"] = 0] = "ERC20";
    PointerType[PointerType["ERC721"] = 1] = "ERC721";
    PointerType[PointerType["NATIVE"] = 2] = "NATIVE";
    PointerType[PointerType["CW20"] = 3] = "CW20";
    PointerType[PointerType["CW721"] = 4] = "CW721";
    PointerType[PointerType["ERC1155"] = 5] = "ERC1155";
    PointerType[PointerType["CW1155"] = 6] = "CW1155";
    PointerType[PointerType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(PointerType || (PointerType = {}));
export var AssetType;
(function (AssetType) {
    AssetType[AssetType["TYPECW20"] = 0] = "TYPECW20";
    AssetType[AssetType["TYPECW721"] = 1] = "TYPECW721";
    AssetType[AssetType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(AssetType || (AssetType = {}));
