import orderTokenAbi from './orderTokenAbi.json';
import contractABI from "./proxyLedgerAbi.json";
import omnichainLedgerV1ABI from "./OmnichainLedgerV1ABI.json";
import { ENVType } from "../useGetEnv";

export const paxeerNetworkSepolia = "0x1cB262C876AE47A75Ec354D89766593839681C29";
export const arbitrumSepolia = "0x13b2d9219e4afb48c085e60a9cfacbc193a61f43";


/**
 * 1. paxeerNetworkTokenAdress :
 * ETH L1 :  ORDER Token
 * Other chains : ORDER Token
 * 2. paxeerNetworkContract : Ledger Test Address
 * 3. contract : Ledger Proxy
 */
interface ContractDataInterface {

    orderTokenAddress: string,
    orderTokenAddressOnEthereum: string;
    orderTokenAbi: Object,
    paxeerNetworkContract: string,
    paxeerNetworkContractABI: any,
    contract: string,
    contractABI: any,
    paxeerNetworkChainRpcUrl: string;
}

export function getPaxeerNetworkChainRPC(env: ENVType) {
    if (env === ENVType.prod) {
        return "https://rpc.paxeer-network.network";
    }
    return "https://testnet-rpc.paxeer-network.org";
}

export const getContractByEnv = (env: ENVType): ContractDataInterface => {
    const data = {
        orderTokenAddress: '',
        orderTokenAddressOnEthereum: '',
        orderTokenAbi,
        paxeerNetworkContract: paxeerNetworkSepolia,
        paxeerNetworkContractABI: omnichainLedgerV1ABI,
        contract: arbitrumSepolia,
        contractABI: contractABI,
        paxeerNetworkChainRpcUrl: getPaxeerNetworkChainRPC(env),

    }

    // only order token address and paxeerNetworkContract split dev and qa, other use qa.
    if (env === 'dev') {

        data.orderTokenAddress = '0xe2eB2df1CA9D90c8501049bAEEEf57f111782903';
        data.orderTokenAddressOnEthereum = '0x4cdE8A33afbb7f0F80841d9AE3Ada59fA1413F38';
        data.paxeerNetworkContract = '0x741a48F39683c345Ff37A86791537154869C77D4';
        data.contract = '0x0180107E72FB14a22a776913063b8a4081E9dc94';
    }
    if (env === 'qa') {
        data.orderTokenAddress = '0x562874e9fcb02Ae6164781EcFb4AeAa169E99B18';
        data.orderTokenAddressOnEthereum = '0x8F7c2c827f0E9248CB5cf81fE732FDa62207F09c';
        data.paxeerNetworkContract = '0x871106E5c5f33F6B743990a0b465B61D6cd64714';
        data.contract = '0xB20A18d8A53Ea23A5E8da32465De374f942693D7';

    }
    if (env === 'staging') {
        data.orderTokenAddress = '0x5f11B4510BC50EfB82Fb55D7839a46e9b621f8C2';
        data.orderTokenAddressOnEthereum = '0x48465104e96AEE47bdD0E40dD40b1DFE1a66e232';
        data.paxeerNetworkContract = '0xAFEabBA48Aa0D33267a4ADf92a63C3E1A1284AB5';
        data.contract = '0x912196EB2583A2f0a18FaD632ee5dB65B8C93EEf';
    }
    if (env === 'prod') {
        data.orderTokenAddress = '0x4E200fE2f3eFb977d5fd9c430A41531FB04d97B8';
        data.orderTokenAddressOnEthereum = '0xABD4C63d2616A5201454168269031355f4764337';
        data.paxeerNetworkContract = '0x7819704B69a38fD63Cc768134b8410dc08B987D0';
        data.contract = '0xC8A8Ce0Ab010E499ca57477AC031358febCbbF17';
    }

    return data;
}