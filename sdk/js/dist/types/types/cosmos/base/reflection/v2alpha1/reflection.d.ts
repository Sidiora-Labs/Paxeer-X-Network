export interface AppDescriptor {
    /**
     * AuthnDescriptor provides information on how to authenticate transactions on the application
     * NOTE: experimental and subject to change in future releases.
     */
    authn?: AuthnDescriptor;
    /** chain provides the chain descriptor */
    chain?: ChainDescriptor;
    /** codec provides metadata information regarding codec related types */
    codec?: CodecDescriptor;
    /** configuration provides metadata information regarding the sdk.Config type */
    configuration?: ConfigurationDescriptor;
    /** query_services provides metadata information regarding the available queriable endpoints */
    query_services?: QueryServicesDescriptor;
    /** tx provides metadata information regarding how to send transactions to the given application */
    tx?: TxDescriptor;
}
export interface TxDescriptor {
    /**
     * fullname is the protobuf fullname of the raw transaction type (for instance the tx.Tx type)
     * it is not meant to support polymorphism of transaction types, it is supposed to be used by
     * reflection clients to understand if they can handle a specific transaction type in an application.
     */
    fullname: string;
    /** msgs lists the accepted application messages (sdk.Msg) */
    msgs: MsgDescriptor[];
}
export interface AuthnDescriptor {
    /** sign_modes defines the supported signature algorithm */
    sign_modes: SigningModeDescriptor[];
}
export interface SigningModeDescriptor {
    /** name defines the unique name of the signing mode */
    name: string;
    /** number is the unique int32 identifier for the sign_mode enum */
    number: number;
    /**
     * authn_info_provider_method_fullname defines the fullname of the method to call to get
     * the metadata required to authenticate using the provided sign_modes
     */
    authn_info_provider_method_fullname: string;
}
export interface ChainDescriptor {
    /** id is the chain id */
    id: string;
}
export interface CodecDescriptor {
    /** interfaces is a list of the registerted interfaces descriptors */
    interfaces: InterfaceDescriptor[];
}
export interface InterfaceDescriptor {
    /** fullname is the name of the interface */
    fullname: string;
    /**
     * interface_accepting_messages contains information regarding the proto messages which contain the interface as
     * google.protobuf.Any field
     */
    interface_accepting_messages: InterfaceAcceptingMessageDescriptor[];
    /** interface_implementers is a list of the descriptors of the interface implementers */
    interface_implementers: InterfaceImplementerDescriptor[];
}
export interface InterfaceImplementerDescriptor {
    /** fullname is the protobuf queryable name of the interface implementer */
    fullname: string;
    /**
     * type_url defines the type URL used when marshalling the type as any
     * this is required so we can provide type safe google.protobuf.Any marshalling and
     * unmarshalling, making sure that we don't accept just 'any' type
     * in our interface fields
     */
    type_url: string;
}
export interface InterfaceAcceptingMessageDescriptor {
    /** fullname is the protobuf fullname of the type containing the interface */
    fullname: string;
    /**
     * field_descriptor_names is a list of the protobuf name (not fullname) of the field
     * which contains the interface as google.protobuf.Any (the interface is the same, but
     * it can be in multiple fields of the same proto message)
     */
    field_descriptor_names: string[];
}
export interface ConfigurationDescriptor {
    /** bech32_account_address_prefix is the account address prefix */
    bech32_account_address_prefix: string;
}
export interface MsgDescriptor {
    /** msg_type_url contains the TypeURL of a sdk.Msg. */
    msg_type_url: string;
}
export type GetAuthnDescriptorRequest = {};
export interface GetAuthnDescriptorResponse {
    /** authn describes how to authenticate to the application when sending transactions */
    authn?: AuthnDescriptor;
}
export type GetChainDescriptorRequest = {};
export interface GetChainDescriptorResponse {
    /** chain describes application chain information */
    chain?: ChainDescriptor;
}
export type GetCodecDescriptorRequest = {};
export interface GetCodecDescriptorResponse {
    /** codec describes the application codec such as registered interfaces and implementations */
    codec?: CodecDescriptor;
}
export type GetConfigurationDescriptorRequest = {};
export interface GetConfigurationDescriptorResponse {
    /** config describes the application's sdk.Config */
    config?: ConfigurationDescriptor;
}
export type GetQueryServicesDescriptorRequest = {};
export interface GetQueryServicesDescriptorResponse {
    /** queries provides information on the available queryable services */
    queries?: QueryServicesDescriptor;
}
export type GetTxDescriptorRequest = {};
export interface GetTxDescriptorResponse {
    /**
     * tx provides information on msgs that can be forwarded to the application
     * alongside the accepted transaction protobuf type
     */
    tx?: TxDescriptor;
}
export interface QueryServicesDescriptor {
    /** query_services is a list of cosmos-sdk QueryServiceDescriptor */
    query_services: QueryServiceDescriptor[];
}
export interface QueryServiceDescriptor {
    /** fullname is the protobuf fullname of the service descriptor */
    fullname: string;
    /** is_module describes if this service is actually exposed by an application's module */
    is_module: boolean;
    /** methods provides a list of query service methods */
    methods: QueryMethodDescriptor[];
}
export interface QueryMethodDescriptor {
    /** name is the protobuf name (not fullname) of the method */
    name: string;
    /**
     * full_query_path is the path that can be used to query
     * this method via tendermint abci.Query
     */
    full_query_path: string;
}
