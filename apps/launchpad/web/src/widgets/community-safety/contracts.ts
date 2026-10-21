export interface SafetyCase {
    ID: string;
    TargetType: string;
    TargetID: string;
    PoolID: string;
    State: 'open' | 'triaged' | 'actioned' | 'dismissed' | 'appealed' | 'final';
    Revision: number;
}
export interface ResolveReportCommand {
    status: 'triaged' | 'actioned' | 'dismissed';
    expected_revision: number;
    reason: string;
    policy_version: string;
}
export interface PrivateAppealOutcome {
    id: string;
    actionId: string;
    state: 'submitted' | 'reviewing' | 'upheld' | 'reversed';
    revision: number;
    reason: string;
    decidedAt: string | null;
}
export type SafetyActionState = {
    state: 'idle';
} | {
    state: 'pending';
} | {
    state: 'acknowledged';
} | {
    state: 'failed';
    status: number;
};
export function actionMessage(action: SafetyActionState): string {
    if (action.state === 'pending')
        return 'Sending...';
    if (action.state === 'acknowledged')
        return 'Got it, thanks';
    if (action.state === 'failed') {
        if (action.status === 409)
            return 'This changed in the meantime. Refresh and try again';
        if (action.status === 401 || action.status === 403)
            return "You can't do this. Try signing in again";
        if (action.status === 429)
            return 'Slow down, try again in a moment';
        return 'Something went wrong, try again';
    }
    return '';
}
