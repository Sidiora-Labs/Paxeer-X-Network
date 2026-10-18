type Env = "prod" | "staging" | "qa" | "dev";

export const VAULTS_CONTRACT_ADDRESSES: Record<Env, Record<string, string>> = {
  prod: {
    vaultProtocol: "0x70Fe7d65Ac7c1a1732f64d2E6fC0E33622D0C991",
    vaultCrossChainManager: "0x58c9747ccAAE56182C7d9c814F5eaca395D8c93B",
    vaultPvLedger: "0xB7E792f0da9104A27288421583748215AefFFd78",
    vaultId:
      "0xa3426a1cef4052c056fced18099be899d93f1427d13b9a1df1806b91fad3d0c2",
    spAddress: "0x8bAA309D93FFFB54A64444FD98E10d92D4d9Eb22",
  },
  staging: {
    vaultProtocol: "0x6B6059259B4096ea6420Eb5e08a22214d2303aE9",
    vaultCrossChainManager: "0x510dD61a988797114A9a51b0d228E894037BD9cb",
    vaultPvLedger: "0x20AFe57C75D1C548A9Da265fBFC5416c43783589",
    vaultId:
      "0x95514fb145354f07bb889f711e856481b5ed52fce52200148aa834b3b29544c8",
    spAddress: "0xbddfd22ef902a4898147a1ca5b985d03c62a8c41",
  },
  qa: {
    vaultProtocol: "0xF5b12d5F1db6DAB8C7c0561152b5e4bb8fD5eb38",
    vaultCrossChainManager: "0xB8a3245407571804b4db5219cBe8D9F2EA1828DE",
    vaultPvLedger: "0x15c9B5705CaCB4eb140dfd7a8c651E18027e5146",
    vaultId:
      "0x4812cbb88f4025372a3e2acd10d02b5f680d7d1fe78091f6cfde80122c861099",
    spAddress: "0xbddfd22ef902a4898147a1ca5b985d03c62a8c41",
  },
  dev: {
    vaultProtocol: "0xA292E1126703F804FBD5671F034c7226538C54C7",
    vaultCrossChainManager: "0x7568ACC147Af12b02713143C3177D7e89C28A9A6",
    vaultPvLedger: "0x2a5909498d85650744dD5CF964F2136962e5AE6E",
    vaultId:
      "0x4812cbb88f4025372a3e2acd10d02b5f680d7d1fe78091f6cfde80122c861099",
    spAddress: "0xbddfd22ef902a4898147a1ca5b985d03c62a8c41",
  },
};
