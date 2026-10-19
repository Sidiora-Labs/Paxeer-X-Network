export interface ParameterChangeProposal {
    title: string;
    description: string;
    changes: ParamChange[];
    is_expedited: boolean;
}
export interface ParamChange {
    subspace: string;
    key: string;
    value: string;
}
