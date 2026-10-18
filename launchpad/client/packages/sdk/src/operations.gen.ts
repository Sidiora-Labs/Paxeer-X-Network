import type { ContractTypes as V2ContractTypes } from '@kindle/sdk-v2/types';
export interface OperationTypes {
"IF_CONNECTED_DEVICES":{input:{};response:V2ContractTypes["ConnectedDevicesResponse"]};
"IF_CONNECTED_ACCOUNT_ASSET_GET":{input:{path:{"wallet":string;"assetId":string}};response:V2ContractTypes["ConnectedAccountAsset"]};
"IF_CONNECTED_ACCOUNT_ASSET_PUT":{input:{path:{"wallet":string;"assetId":string};body:Blob};response:V2ContractTypes["ConnectedAccountAsset"]};
"IF_MODERATE":{input:{body:V2ContractTypes["ModerateBody"]};response:V2ContractTypes["ModerateResponse"]};
"IF_REPORT_LIST":{input:{query:{"state":"open"|"reviewing"|"resolved"|"dismissed";"cursor"?:string;"limit"?:number}};response:V2ContractTypes["ReportListResponse"]};
"IF_UPLOAD_CREATE":{input:{body:V2ContractTypes["UploadCreateBody"]};response:V2ContractTypes["UploadCreateResponse"]};
"IF_ASSET_GET":{input:{path:{"assetId":string}};response:V2ContractTypes["AssetGetResponse"]};
"IF_UPLOAD_COMPLETE":{input:{path:{"assetId":string};body:V2ContractTypes["UploadCompleteBody"]};response:V2ContractTypes["UploadCompleteResponse"]};
"IF_CHALLENGE":{input:{body:V2ContractTypes["ChallengeBody"]};response:V2ContractTypes["ChallengeResponse"]};
"IF_SESSIONS":{input:{query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["SessionsResponse"]};
"IF_LOGIN":{input:{body:V2ContractTypes["LoginBody"]};response:V2ContractTypes["LoginResponse"]};
"IF_LOGOUT":{input:{path:{"sessionId":string}};response:undefined};
"IF_REFRESH":{input:{};response:V2ContractTypes["RefreshResponse"]};
"IF_AUTOMATION_PUBLIC_DISCLOSURE":{input:{path:{"campaignId":string}};response:V2ContractTypes["AutomationPublicDisclosureResponse"]};
"IF_BOUNTY_LIST":{input:{query?:{"state"?:"open"|"reviewing"|"paid";"poolAddress"?:string;"cursor"?:string;"limit"?:number}};response:V2ContractTypes["BountyListResponse"]};
"IF_BOUNTY_CREATE":{input:{body:V2ContractTypes["BountyCreateBody"]};response:V2ContractTypes["BountyCreateResponse"]};
"IF_BOUNTIES_STATS":{input:{};response:V2ContractTypes["BountiesStatsResponse"]};
"IF_BOUNTY_GET":{input:{path:{"bountyId":string}};response:V2ContractTypes["BountyGetResponse"]};
"IF_BOUNTY_SUBMISSIONS":{input:{path:{"bountyId":string};query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["BountySubmissionsResponse"]};
"IF_BOUNTY_SUBMIT":{input:{path:{"bountyId":string};body:V2ContractTypes["BountySubmitBody"]};response:V2ContractTypes["BountySubmitResponse"]};
"IF_BOUNTY_ACCEPT":{input:{path:{"bountyId":string;"submissionId":string};body:V2ContractTypes["BountyAcceptBody"]};response:V2ContractTypes["BountyAcceptResponse"]};
"IF_CALLOUT_CREATE":{input:{body:V2ContractTypes["CalloutCreateBody"]};response:V2ContractTypes["CalloutCreateResponse"]};
"IF_CONNECTED_CHAT_CONVERSATIONS":{input:{};response:V2ContractTypes["ConnectedChatConversationsResponse"]};
"IF_CONNECTED_CHAT_HISTORY":{input:{query:{"conversation_id":string;"cursor"?:string;"limit"?:number}};response:V2ContractTypes["ConnectedFanoutHistoryPage"]};
"IF_CONNECTED_CHAT_READ":{input:{body:V2ContractTypes["ConnectedChatReadBody"]};response:V2ContractTypes["ConnectedSuccessResponse"]};
"IF_CONNECTED_CHAT_REVISE":{input:{body:V2ContractTypes["ConnectedChatReviseBody"]};response:V2ContractTypes["ConnectedFanoutDurableMessage"]};
"IF_COMMENT_DELETE":{input:{path:{"commentId":string}};response:undefined};
"IF_COMMENT_EDIT":{input:{path:{"commentId":string};body:V2ContractTypes["CommentEditBody"]};response:V2ContractTypes["CommentEditResponse"]};
"IF_COMMENT_UNLIKE":{input:{path:{"commentId":string}};response:undefined};
"IF_COMMENT_LIKE":{input:{path:{"commentId":string}};response:V2ContractTypes["CommentLikeResponse"]};
"IF_CUP_CURRENT":{input:{};response:V2ContractTypes["CupCurrentResponse"]};
"IF_CUP_GET":{input:{path:{"cupId":string}};response:V2ContractTypes["CupGetResponse"]};
"IF_CUP_RESULTS":{input:{path:{"cupId":string}};response:V2ContractTypes["CupResultsResponse"]};
"IF_CUP_STANDINGS":{input:{path:{"cupId":string};query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["CupStandingsResponse"]};
"IF_CONNECTED_DISCOVERY_ALERT":{input:{path:{"id":string};body:V2ContractTypes["ConnectedAlertPutBody"]};response:V2ContractTypes["ConnectedDiscoveryAlertRule"]};
"IF_CONNECTED_DISCOVERY_CALLOUT_CREATE":{input:{body:V2ContractTypes["ConnectedCalloutCreateBody"]};response:V2ContractTypes["ConnectedDiscoveryCallout"]};
"IF_CONNECTED_DISCOVERY_CALLOUT_EDIT":{input:{path:{"id":string};body:V2ContractTypes["ConnectedCalloutEditBody"]};response:V2ContractTypes["ConnectedDiscoveryCallout"]};
"IF_CONNECTED_FEED":{input:{query?:{"order"?:"chronological"|"relevance";"cursor"?:string;"limit"?:number}};response:V2ContractTypes["ConnectedDiscoveryFeedPage"]};
"IF_CONNECTED_FEED_DETAIL":{input:{path:{"id":string}};response:V2ContractTypes["ConnectedDiscoveryFeedCard"]};
"IF_CONNECTED_DISCOVERY_LEADERBOARDS":{input:{query:{"start":string;"end":string}};response:V2ContractTypes["ConnectedDiscoveryLeaderboardV2"]};
"IF_CONNECTED_NOTIFICATION_LIST":{input:{};response:V2ContractTypes["ConnectedNotificationListResponse"]};
"IF_CONNECTED_NOTIFICATION_DELETE":{input:{path:{"id":string}};response:V2ContractTypes["ConnectedDeletedResponse"]};
"IF_CONNECTED_NOTIFICATION_READ":{input:{path:{"id":string};body:V2ContractTypes["ConnectedBooleanReadBody"]};response:V2ContractTypes["ConnectedReadResponse"]};
"IF_CONNECTED_DISCOVERY_SQUAD_CREATE":{input:{body:V2ContractTypes["ConnectedSquadCreateBody"]};response:V2ContractTypes["ConnectedDiscoverySquad"]};
"IF_CONNECTED_DISCOVERY_SQUAD_PATCH":{input:{path:{"id":string};body:V2ContractTypes["ConnectedSquadPatchBody"]};response:V2ContractTypes["ConnectedDiscoverySquad"]};
"IF_CONNECTED_DISCOVERY_SQUAD_ACCEPT":{input:{path:{"id":string};body:V2ContractTypes["ConnectedSquadAcceptBody"]};response:V2ContractTypes["ConnectedDiscoveryMembership"]};
"IF_CONNECTED_DISCOVERY_SQUAD_INVITE":{input:{path:{"id":string};body:V2ContractTypes["ConnectedSquadInviteBody"]};response:V2ContractTypes["ConnectedInvitedResponse"]};
"IF_CONNECTED_DISCOVERY_SQUAD_MEMBER":{input:{path:{"id":string;"account":string};body:V2ContractTypes["ConnectedSquadMemberBody"]};response:V2ContractTypes["ConnectedUpdatedResponse"]};
"IF_CONNECTED_DISCOVERY_SQUAD_TRANSFER":{input:{path:{"id":string};body:V2ContractTypes["ConnectedSquadTransferBody"]};response:V2ContractTypes["ConnectedAcceptedResponse"]};
"IF_EVENTS_SSE":{input:{query:{"topics":Array<string>}};response:Response};
"IF_FUNDING_RAILS":{input:{query:{"chainId":string;"assetAddress"?:string}};response:V2ContractTypes["FundingRailsResponse"]};
"IF_HOLDER_REWARDS_POOLS":{input:{query?:{"sort"?:"rewards"|"mcap"|"holders"|"age";"cursor"?:string;"limit"?:number}};response:V2ContractTypes["HolderRewardsPoolsResponse"]};
"IF_HOLDER_REWARDS_STATS":{input:{};response:V2ContractTypes["HolderRewardsStatsResponse"]};
"IF_LEADERBOARDS":{input:{path:{"category":"creator"|"trader"|"volume"};query:{"window":"24h"|"7d"|"30d";"cursor"?:string;"limit"?:number}};response:V2ContractTypes["LeaderboardsResponse"]};
"IF_ALERT_CREATE":{input:{body:V2ContractTypes["AlertCreateBody"]};response:V2ContractTypes["AlertCreateResponse"]};
"IF_ALERT_DELETE":{input:{path:{"alertId":string}};response:undefined};
"IF_AUTOMATION_CREATE":{input:{body:V2ContractTypes["AutomationCreateBody"]};response:V2ContractTypes["AutomationCreateResponse"]};
"IF_AUTOMATION_STATUS":{input:{path:{"campaignId":string}};response:V2ContractTypes["AutomationStatusResponse"]};
"IF_AUTOMATION_ACTION":{input:{path:{"campaignId":string};body:V2ContractTypes["AutomationActionBody"]};response:V2ContractTypes["AutomationActionResponse"]};
"IF_DM_LIST":{input:{query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["DmListResponse"]};
"IF_DM_CREATE":{input:{body:V2ContractTypes["DmCreateBody"]};response:V2ContractTypes["DmCreateResponse"]};
"IF_ACCOUNT_DELETE":{input:{body:V2ContractTypes["AccountDeleteBody"]};response:V2ContractTypes["AccountDeleteResponse"]};
"IF_ACCOUNT_DELETE_STATUS":{input:{path:{"jobId":string}};response:V2ContractTypes["AccountDeleteStatusResponse"]};
"IF_DEVICE_REVOKE":{input:{path:{"deviceId":string}};response:undefined};
"IF_DEVICE_REGISTER":{input:{path:{"deviceId":string};body:V2ContractTypes["DeviceRegisterBody"]};response:V2ContractTypes["DeviceRegisterResponse"]};
"IF_ACCOUNT_EXPORT":{input:{body:V2ContractTypes["AccountExportBody"]};response:V2ContractTypes["AccountExportResponse"]};
"IF_ACCOUNT_EXPORT_STATUS":{input:{path:{"jobId":string}};response:V2ContractTypes["AccountExportStatusResponse"]};
"IF_UNFOLLOW":{input:{path:{"wallet":string}};response:undefined};
"IF_FOLLOW":{input:{path:{"wallet":string}};response:V2ContractTypes["FollowResponse"]};
"IF_DEPOSIT_INTENT":{input:{body:V2ContractTypes["DepositIntentBody"]};response:V2ContractTypes["DepositIntentResponse"]};
"IF_WITHDRAW_STATUS":{input:{path:{"preparationId":string};query?:{"transactionHash"?:string}};response:V2ContractTypes["WithdrawStatusResponse"]};
"IF_WITHDRAW_PREPARE":{input:{body:V2ContractTypes["WithdrawPrepareBody"]};response:V2ContractTypes["WithdrawPrepareResponse"]};
"IF_FUNDING_STATUS":{input:{path:{"fundingId":string}};response:V2ContractTypes["FundingStatusResponse"]};
"IF_NOTIFICATIONS":{input:{query?:{"unreadOnly"?:boolean;"cursor"?:string;"limit"?:number}};response:V2ContractTypes["NotificationsResponse"]};
"IF_NOTIFICATION_READ":{input:{path:{"notificationId":string}};response:V2ContractTypes["NotificationReadResponse"]};
"IF_ORDER_LIST":{input:{query?:{"status"?:"active"|"filled"|"cancelled"|"failed"|"expired";"cursor"?:string;"limit"?:number}};response:V2ContractTypes["OrderListResponse"]};
"IF_ORDER_CREATE":{input:{body:V2ContractTypes["OrderCreateBody"]};response:V2ContractTypes["OrderCreateResponse"]};
"IF_ORDER_CANCEL":{input:{path:{"orderId":string}};response:V2ContractTypes["OrderCancelResponse"]};
"IF_PORTFOLIO":{input:{query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["PortfolioResponse"]};
"IF_CONNECTED_PORTFOLIO_CARD_CREATE":{input:{body:V2ContractTypes["ConnectedPortfolioCardBody"]};response:V2ContractTypes["ConnectedL17StoredCard"]};
"IF_CONNECTED_PORTFOLIO_CARD_REVOKE":{input:{path:{"cardId":string}};response:V2ContractTypes["ConnectedStateResponse"]};
"IF_CONNECTED_PORTFOLIO_CARD_STATUS":{input:{path:{"cardId":string}};response:V2ContractTypes["ConnectedL17StoredCard"]};
"IF_CONNECTED_PORTFOLIO_EXPORT_CREATE":{input:{body:V2ContractTypes["ConnectedPortfolioExportBody"]};response:V2ContractTypes["ConnectedL17ExportJob"]};
"IF_CONNECTED_PORTFOLIO_EXPORT_CANCEL":{input:{path:{"jobId":string}};response:V2ContractTypes["ConnectedStateResponse"]};
"IF_CONNECTED_PORTFOLIO_EXPORT_STATUS":{input:{path:{"jobId":string}};response:V2ContractTypes["ConnectedL17ExportJob"]};
"IF_CONNECTED_PORTFOLIO_DOWNLOAD":{input:{path:{"jobId":string}};response:Response};
"IF_PNL_HISTORY":{input:{query:{"from":string;"to":string;"interval":"1h"|"1d";"limit"?:number}};response:V2ContractTypes["PnlHistoryResponse"]};
"IF_PREFERENCES_READ":{input:{};response:V2ContractTypes["PreferencesReadResponse"]};
"IF_PREFERENCES_UPDATE":{input:{body:V2ContractTypes["PreferencesUpdateBody"]};response:V2ContractTypes["PreferencesUpdateResponse"]};
"IF_CONNECTED_REFERRAL_DASHBOARD":{input:{query?:{"campaign"?:string;"policyVersion"?:string}};response:V2ContractTypes["ConnectedL18ReferralDashboard"]};
"IF_CONNECTED_REFERRAL_BIND":{input:{query?:{"campaign"?:string;"policyVersion"?:string};body:V2ContractTypes["ConnectedReferralBindBody"]};response:V2ContractTypes["ConnectedL18Binding"]};
"IF_CONNECTED_REFERRAL_CLICK":{input:{query?:{"campaign"?:string;"policyVersion"?:string};body:V2ContractTypes["ConnectedReferralClickBody"]};response:V2ContractTypes["ConnectedL18ReferralClick"]};
"IF_CONNECTED_REFERRAL_CONSENT":{input:{query?:{"campaign"?:string;"policyVersion"?:string};body:V2ContractTypes["ConnectedReferralConsentBody"]};response:V2ContractTypes["ConnectedL18ReferralConsent"]};
"IF_CONNECTED_REFERRAL_CODE":{input:{query?:{"campaign"?:string;"policyVersion"?:string};body:V2ContractTypes["ConnectedReferralCodeBody"]};response:V2ContractTypes["ConnectedL18ReferralCode"]};
"IF_REWARDS":{input:{query?:{"programId"?:string;"cursor"?:string;"limit"?:number}};response:V2ContractTypes["RewardsResponse"]};
"IF_REWARD_CLAIM":{input:{body:V2ContractTypes["RewardClaimBody"]};response:V2ContractTypes["RewardClaimResponse"]};
"IF_WALLET_LINK_CHALLENGE":{input:{body:V2ContractTypes["WalletLinkChallengeBody"]};response:V2ContractTypes["WalletLinkChallengeResponse"]};
"IF_WALLET_LINK_REMOVE":{input:{path:{"linkId":string};body:V2ContractTypes["WalletLinkRemoveBody"]};response:undefined};
"IF_WALLET_LINK_VERIFY":{input:{path:{"linkId":string};body:V2ContractTypes["WalletLinkVerifyBody"]};response:V2ContractTypes["WalletLinkVerifyResponse"]};
"IF_WATCHLIST_GET":{input:{query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["WatchlistGetResponse"]};
"IF_WATCHLIST_DELETE":{input:{path:{"poolAddress":string}};response:undefined};
"IF_WATCHLIST_PUT":{input:{path:{"poolAddress":string};body:V2ContractTypes["WatchlistPutBody"]};response:V2ContractTypes["WatchlistPutResponse"]};
"IF_NARRATIVES":{input:{query:{"cursor"?:string;"limit"?:number;"status":string}};response:V2ContractTypes["NarrativesResponse"]};
"IF_NARRATIVE_FEED":{input:{path:{"narrativeId":string};query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["NarrativeFeedResponse"]};
"IF_CONNECTED_NETWORK_AUTHORITY":{input:{};response:V2ContractTypes["ConnectedNetworkAuthorityResponse"]};
"IF_DEPLOYMENT":{input:{};response:V2ContractTypes["DeploymentResponse"]};
"IF_OPERATOR_DLQ":{input:{query:{"stream":string;"consumer":string;"cursor"?:string;"limit"?:number}};response:V2ContractTypes["OperatorDlqResponse"]};
"IF_OPERATOR_REDRIVE":{input:{body:V2ContractTypes["OperatorRedriveBody"]};response:V2ContractTypes["OperatorRedriveResponse"]};
"IF_OPERATOR_HEALTH":{input:{query:{"services":Array<string>}};response:V2ContractTypes["OperatorHealthResponse"]};
"IF_OPERATOR_JOB_STATUS":{input:{path:{"jobId":string}};response:V2ContractTypes["OperatorJobStatusResponse"]};
"IF_OPERATOR_METRICS":{input:{query:{"metric":string;"service":string;"from":string;"to":string;"stepSeconds":number}};response:V2ContractTypes["OperatorMetricsResponse"]};
"IF_OPERATOR_REPLAY":{input:{body:V2ContractTypes["OperatorReplayBody"]};response:V2ContractTypes["OperatorReplayResponse"]};
"IF_PLATFORM":{input:{query:{"window":"24h"|"7d"|"30d"}};response:V2ContractTypes["PlatformResponse"]};
"IF_POOL_LIST":{input:{query:{"sort":"new"|"volume"|"trending";"cursor"?:string;"limit"?:number}};response:V2ContractTypes["PoolListResponse"]};
"IF_STATS_BATCH":{input:{body:V2ContractTypes["StatsBatchBody"]};response:V2ContractTypes["StatsBatchResponse"]};
"IF_POOL_DETAIL":{input:{path:{"poolAddress":string}};response:V2ContractTypes["PoolDetailResponse"]};
"IF_CALLOUT_LIST":{input:{path:{"poolAddress":string};query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["CalloutListResponse"]};
"IF_CANDLES":{input:{path:{"poolAddress":string};query:{"interval":"1m"|"5m"|"15m"|"1h"|"4h"|"1d"|"1w";"from":string;"to":string;"cursor"?:string|null;"limit"?:number}};response:V2ContractTypes["CandlesResponse"]};
"IF_COMMENTS_LIST":{input:{path:{"poolAddress":string};query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["CommentsListResponse"]};
"IF_COMMENT_CREATE":{input:{path:{"poolAddress":string};body:V2ContractTypes["CommentCreateBody"]};response:V2ContractTypes["CommentCreateResponse"]};
"IF_HOLDERS":{input:{path:{"poolAddress":string};query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["HoldersResponse"]};
"IF_STATS":{input:{path:{"poolAddress":string}};response:V2ContractTypes["StatsResponse"]};
"IF_TRADES":{input:{path:{"poolAddress":string};query?:{"side"?:"buy"|"sell";"cursor"?:string;"limit"?:number}};response:V2ContractTypes["TradesResponse"]};
"IF_CONNECTED_PORTFOLIO_CARD_IMAGE":{input:{path:{"cardId":string}};response:Response};
"IF_CONNECTED_PREFLIGHT_CLAIM":{input:{body:V2ContractTypes["ConnectedPreflightClaimBody"]};response:V2ContractTypes["ConnectedCapabilitiesPreflight"]};
"IF_CONNECTED_PREFLIGHT_CREATE":{input:{body:V2ContractTypes["ConnectedPreflightCreateBody"]};response:V2ContractTypes["ConnectedCapabilitiesPreflight"]};
"IF_CONNECTED_PREFLIGHT_QUOTE":{input:{body:V2ContractTypes["ConnectedPreflightQuoteBody"]};response:V2ContractTypes["ConnectedCapabilitiesPreflight"]};
"IF_CONNECTED_PREFLIGHT_TRADE":{input:{body:V2ContractTypes["ConnectedPreflightTradeBody"]};response:V2ContractTypes["ConnectedCapabilitiesPreflight"]};
"IF_PROFILE_GET":{input:{path:{"wallet":string}};response:V2ContractTypes["ProfileGetResponse"]};
"IF_PROFILE_PATCH":{input:{path:{"wallet":string};body:V2ContractTypes["ProfilePatchBody"]};response:V2ContractTypes["ProfilePatchResponse"]};
"IF_FOLLOW_STATS":{input:{path:{"wallet":string}};response:V2ContractTypes["FollowStatsResponse"]};
"IF_FOLLOWERS":{input:{path:{"wallet":string};query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["FollowersResponse"]};
"IF_PROFILES_BATCH":{input:{query:{"wallets":Array<string>}};response:V2ContractTypes["ProfilesBatchResponse"]};
"IF_LIVE_WEBHOOK":{input:{body:V2ContractTypes["LiveWebhookBody"]};response:V2ContractTypes["LiveWebhookResponse"]};
"IF_QUOTE":{input:{body:V2ContractTypes["QuoteBody"]};response:V2ContractTypes["QuoteResponse"]};
"IF_RANKINGS":{input:{path:{"category":"trending"|"breakout"|"volume"|"gainers"|"losers"|"movers"|"unusual"|"new"};query?:{"cursor"?:string|null;"limit"?:number}};response:V2ContractTypes["RankingsResponse"]};
"IF_REPORT_CREATE":{input:{body:V2ContractTypes["ReportCreateBody"]};response:V2ContractTypes["ReportCreateResponse"]};
"IF_MESSAGES_LIST":{input:{path:{"roomId":string};query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["MessagesListResponse"]};
"IF_MESSAGE_SEND":{input:{path:{"roomId":string};body:V2ContractTypes["MessageSendBody"]};response:V2ContractTypes["MessageSendResponse"]};
"IF_CONNECTED_SAFETY_APPEAL":{input:{body:V2ContractTypes["ConnectedSafetyAppealBody"]};response:V2ContractTypes["ConnectedSafetyAppealOutcome"]};
"IF_CONNECTED_SAFETY_APPEAL_DETAIL":{input:{path:{"appealId":string}};response:V2ContractTypes["ConnectedSafetyAppealOutcome"]};
"IF_CONNECTED_SAFETY_REVERSE_BAN":{input:{path:{"appealId":string};body:V2ContractTypes["ConnectedSafetyReversalCommand"]};response:V2ContractTypes["ConnectedOkResponse"]};
"IF_CONNECTED_SAFETY_CASES":{input:{query?:{"state"?:string;"after"?:string;"limit"?:number}};response:V2ContractTypes["ConnectedSafetyCasesResponse"]};
"IF_CONNECTED_SAFETY_TRANSITION":{input:{path:{"caseId":string};body:V2ContractTypes["ConnectedSafetyTransitionBody"]};response:V2ContractTypes["ConnectedRevisionResponse"]};
"IF_CONNECTED_SAFETY_BAN":{input:{path:{"caseId":string};body:V2ContractTypes["ConnectedSafetyBanBody"]};response:V2ContractTypes["ConnectedSafetyBanResponse"]};
"IF_CONNECTED_SAFETY_RESTORE":{input:{path:{"caseId":string};body:V2ContractTypes["ConnectedSafetyRestoreCommand"]};response:V2ContractTypes["ConnectedSafetyVisibility"]};
"IF_CONNECTED_SAFETY_REPORT":{input:{body:V2ContractTypes["ConnectedSafetyReportBody"]};response:V2ContractTypes["ConnectedSafetyCase"]};
"IF_SEARCH":{input:{query:{"q":string;"type":"pool"|"token"|"profile";"limit"?:number}};response:V2ContractTypes["SearchResponse"]};
"IF_CONNECTED_EXPANDED_SEARCH":{input:{query:{"q":string;"type":"pool"|"token"|"profile";"limit"?:number;"sort"?:"address";"filter"?:string;"cursor"?:string}};response:V2ContractTypes["ConnectedExpandedSearchResponse"]};
"IF_TOPIC_SNAPSHOT_READ":{input:{query:{"topics":Array<string>;"normalizedFilters"?:V2ContractTypes["InputTopicFilters"];"cursor"?:V2ContractTypes["OpaqueResumeCursor"]|null}};response:V2ContractTypes["TopicSnapshotReadResponse"]};
"IF_SQUAD_CREATE":{input:{body:V2ContractTypes["SquadCreateBody"]};response:V2ContractTypes["SquadCreateResponse"]};
"IF_SQUAD_READ":{input:{path:{"squadId":string}};response:V2ContractTypes["SquadReadResponse"]};
"IF_SQUAD_MEMBERSHIP":{input:{path:{"squadId":string;"accountId":string};body:V2ContractTypes["SquadMembershipBody"]};response:V2ContractTypes["SquadMembershipResponse"]};
"IF_STREAM_TICKET_ISSUE":{input:{body:V2ContractTypes["StreamTicketIssueBody"]};response:V2ContractTypes["StreamTicketIssueResponse"]};
"IF_STREAM_LIST":{input:{query?:{"status"?:string;"poolAddress"?:string;"cursor"?:string;"limit"?:number}};response:V2ContractTypes["StreamListResponse"]};
"IF_STREAM_CREATE":{input:{body:V2ContractTypes["StreamCreateBody"]};response:V2ContractTypes["StreamCreateResponse"]};
"IF_STREAM_OPERATION_GET":{input:{path:{"operationId":string}};response:V2ContractTypes["StreamOperationGetResponse"]};
"IF_STREAM_INGEST_CREDENTIAL":{input:{path:{"streamId":string}};response:V2ContractTypes["StreamIngestCredentialResponse"]};
"IF_STREAM_HEARTBEAT":{input:{path:{"streamId":string};body:V2ContractTypes["StreamHeartbeatBody"]};response:V2ContractTypes["StreamHeartbeatResponse"]};
"IF_STREAM_END":{input:{path:{"streamId":string}};response:V2ContractTypes["StreamEndResponse"]};
"IF_STREAM_START":{input:{path:{"streamId":string};body:V2ContractTypes["StreamStartBody"]};response:V2ContractTypes["StreamStartResponse"]};
"IF_CONNECTED_TOKEN_ASSET_GET":{input:{path:{"tokenAddress":string;"assetId":string}};response:V2ContractTypes["ConnectedAssetResponse"]};
"IF_CONNECTED_TOKEN_ASSET_PUT":{input:{path:{"tokenAddress":string;"assetId":string};body:Blob};response:V2ContractTypes["ConnectedAssetResponse"]};
"IF_CONNECTED_TOKEN_MEDIA":{input:{path:{"tokenAddress":string}};response:V2ContractTypes["ConnectedTokenMediaResponse"]};
"IF_METADATA_GET":{input:{path:{"tokenAddress":string}};response:V2ContractTypes["MetadataGetResponse"]};
"IF_METADATA_PATCH":{input:{path:{"tokenAddress":string};body:V2ContractTypes["MetadataPatchBody"]};response:V2ContractTypes["MetadataPatchResponse"]};
"IF_TX_STATUS":{input:{path:{"transactionHash":string}};response:V2ContractTypes["TxStatusResponse"]};
"IF_VOICE_CREATE":{input:{body:V2ContractTypes["VoiceCreateBody"]};response:V2ContractTypes["VoiceCreateResponse"]};
"IF_VOICE_READ":{input:{path:{"roomId":string}};response:V2ContractTypes["VoiceReadResponse"]};
"IF_CONNECTED_VOICE_END":{input:{path:{"roomId":string};body:V2ContractTypes["ConnectedVoiceExpectedBody"]};response:V2ContractTypes["ConnectedVoiceRoomResponse"]};
"IF_CONNECTED_VOICE_LEAVE":{input:{path:{"roomId":string}};response:V2ContractTypes["ConnectedOkResponse"]};
"IF_VOICE_MODERATE":{input:{path:{"roomId":string};body:V2ContractTypes["VoiceModerateBody"]};response:V2ContractTypes["VoiceModerateResponse"]};
"IF_CONNECTED_VOICE_PROMPT":{input:{path:{"roomId":string}};response:V2ContractTypes["ConnectedVoicePromptResponse"]};
"IF_CONNECTED_VOICE_RECORDING_GET":{input:{path:{"roomId":string}};response:V2ContractTypes["ConnectedVoiceRecording"]};
"IF_CONNECTED_VOICE_RECORDING_POST":{input:{path:{"roomId":string}};response:V2ContractTypes["ConnectedVoiceRecording"]};
"IF_CONNECTED_VOICE_RECORDING_CONSENT":{input:{path:{"roomId":string};body:V2ContractTypes["ConnectedVoiceRecordingConsentBody"]};response:V2ContractTypes["ConnectedOkResponse"]};
"IF_CONNECTED_VOICE_RECORDING_START":{input:{path:{"roomId":string};body:V2ContractTypes["ConnectedVoiceExpectedBody"]};response:V2ContractTypes["ConnectedVoiceRecording"]};
"IF_CONNECTED_VOICE_RECORDING_STOP":{input:{path:{"roomId":string};body:V2ContractTypes["ConnectedVoiceExpectedBody"]};response:V2ContractTypes["ConnectedVoiceRecording"]};
"IF_CONNECTED_VOICE_SPEAKER_REQUESTS":{input:{path:{"roomId":string};body:V2ContractTypes["ConnectedVoiceExpectedBody"]};response:V2ContractTypes["ConnectedOkResponse"]};
"IF_CONNECTED_VOICE_SPEAKERS":{input:{path:{"roomId":string};body:V2ContractTypes["ConnectedVoiceSpeakerBody"]};response:V2ContractTypes["ConnectedOkResponse"]};
"IF_CONNECTED_VOICE_START":{input:{path:{"roomId":string};body:V2ContractTypes["ConnectedVoiceExpectedBody"]};response:V2ContractTypes["ConnectedVoiceRoomResponse"]};
"IF_VOICE_JOIN_TICKET":{input:{path:{"roomId":string};body:V2ContractTypes["VoiceJoinTicketBody"]};response:V2ContractTypes["VoiceJoinTicketResponse"]};
"IF_VOICE_END":{input:{path:{"roomId":string};body:V2ContractTypes["VoiceEndBody"]};response:V2ContractTypes["VoiceEndResponse"]};
"IF_VOICE_LEAVE":{input:{path:{"roomId":string};body:V2ContractTypes["VoiceLeaveBody"]};response:V2ContractTypes["VoiceLeaveResponse"]};
"IF_CONNECTED_PNL_INTERVAL_V3":{input:{query:{"from":string;"to":string;"interval"?:"1h"|"1d";"limit"?:number}};response:V2ContractTypes["ConnectedL16IntervalSeries"]};
"IF_CONNECTED_PORTFOLIO_V3":{input:{query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["ConnectedPortfolioV3Response"]};
"IF_CONNECTED_PORTFOLIO_INTERVAL_V3":{input:{query:{"from":string;"to":string;"interval"?:"1h"|"1d";"limit"?:number}};response:V2ContractTypes["ConnectedL16IntervalSeries"]};
"IF_CONNECTED_SHARE_V3_PUBLISH":{input:{body:V2ContractTypes["ConnectedL17SharePublishV3"]};response:V2ContractTypes["ConnectedL17OwnerShareV3"]};
"IF_CONNECTED_SHARE_V3_PREVIEW":{input:{body:V2ContractTypes["ConnectedL17ShareSelectionV3"]};response:V2ContractTypes["ConnectedL17SharePreviewV3"]};
"IF_CONNECTED_SHARE_V3_REVOKE":{input:{path:{"shareId":string}};response:V2ContractTypes["ConnectedL17OwnerShareV3"]};
"IF_CONNECTED_SHARE_V3_STATUS":{input:{path:{"shareId":string}};response:V2ContractTypes["ConnectedL17OwnerShareV3"]};
"IF_CONNECTED_EXTENDED_PREFERENCES_GET":{input:{};response:V2ContractTypes["ConnectedExtendedPreferencesResponse"]};
"IF_CONNECTED_EXTENDED_PREFERENCES_PATCH":{input:{body:V2ContractTypes["ConnectedExtendedPreferencesBody"]};response:V2ContractTypes["ConnectedExtendedPreferencesResponse"]};
"IF_CONNECTED_SHARE_V3_PUBLIC":{input:{path:{"shareId":string}};response:V2ContractTypes["ConnectedL17PublicShareV3"]};
"IF_CONNECTED_SHARE_V3_IMAGE":{input:{path:{"shareId":string}};response:Response};
"IF_CONNECTED_EXTENDED_PROFILE_GET":{input:{path:{"wallet":string}};response:V2ContractTypes["ConnectedExtendedProfileResponse"]};
"IF_CONNECTED_EXTENDED_PROFILE_PATCH":{input:{path:{"wallet":string};body:V2ContractTypes["ConnectedExtendedProfileBody"]};response:V2ContractTypes["ConnectedExtendedProfileResponse"]};
"IF_CONNECTED_VOICE_V3_CREATE":{input:{body:V2ContractTypes["ConnectedVoiceCreateBody"]};response:V2ContractTypes["ConnectedVoiceRoomResponse"]};
"IF_CONNECTED_VOICE_V3_MODERATE":{input:{path:{"roomId":string};body:V2ContractTypes["ConnectedVoiceModerationBody"]};response:V2ContractTypes["ConnectedOkResponse"]};
"IF_CONNECTED_VOICE_V3_TICKET":{input:{path:{"roomId":string};body:V2ContractTypes["ConnectedVoiceTicketBody"]};response:V2ContractTypes["ConnectedVoiceTicketResponse"]};
"IF_CONNECTED_PUBLIC_PORTFOLIO_V3":{input:{path:{"wallet":string};query?:{"cursor"?:string;"limit"?:number}};response:V2ContractTypes["ConnectedPortfolioV3Response"]}
}
export const operationDefinitions = {
  "IF_ACCOUNT_DELETE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "AccountDeleteBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/me/deletion-requests",
    "public": false,
    "response": "AccountDeleteResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_ACCOUNT_DELETE_STATUS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "jobId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      },
      {
        "in": "header",
        "name": "X-Recovery-Receipt",
        "required": false,
        "schema": {
          "maxLength": 4096,
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/deletion-requests/{jobId}",
    "public": false,
    "response": "AccountDeleteStatusResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_ACCOUNT_EXPORT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "AccountExportBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/me/exports",
    "public": false,
    "response": "AccountExportResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_ACCOUNT_EXPORT_STATUS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "jobId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/exports/{jobId}",
    "public": false,
    "response": "AccountExportStatusResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_ALERT_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "AlertCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/me/alerts",
    "public": false,
    "response": "AlertCreateResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_ALERT_DELETE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": true,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "DELETE",
    "parameters": [
      {
        "in": "path",
        "name": "alertId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/alerts/{alertId}",
    "public": false,
    "response": null,
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_ASSET_GET": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "assetId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/assets/{assetId}",
    "public": false,
    "response": "AssetGetResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_AUTOMATION_ACTION": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "AutomationActionBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "campaignId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/automation-campaigns/{campaignId}/actions",
    "public": false,
    "response": "AutomationActionResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_AUTOMATION_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "AutomationCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/me/automation-campaigns",
    "public": false,
    "response": "AutomationCreateResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_AUTOMATION_PUBLIC_DISCLOSURE": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "campaignId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/automation-campaigns/{campaignId}/disclosure",
    "public": false,
    "response": "AutomationPublicDisclosureResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_AUTOMATION_STATUS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "campaignId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/automation-campaigns/{campaignId}",
    "public": false,
    "response": "AutomationStatusResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_BOUNTIES_STATS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [],
    "path": "/v2/bounties/stats",
    "public": true,
    "response": "BountiesStatsResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_BOUNTY_ACCEPT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "BountyAcceptBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "bountyId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      },
      {
        "in": "path",
        "name": "submissionId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/bounties/{bountyId}/submissions/{submissionId}:accept",
    "public": false,
    "response": "BountyAcceptResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_BOUNTY_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "BountyCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/bounties",
    "public": false,
    "response": "BountyCreateResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_BOUNTY_GET": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "bountyId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/bounties/{bountyId}",
    "public": true,
    "response": "BountyGetResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_BOUNTY_LIST": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "state",
        "required": false,
        "schema": {
          "enum": [
            "open",
            "reviewing",
            "paid"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "poolAddress",
        "required": false,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/bounties",
    "public": true,
    "response": "BountyListResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_BOUNTY_SUBMISSIONS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "bountyId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/bounties/{bountyId}/submissions",
    "public": true,
    "response": "BountySubmissionsResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_BOUNTY_SUBMIT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "BountySubmitBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "bountyId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/bounties/{bountyId}/submissions",
    "public": false,
    "response": "BountySubmitResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CALLOUT_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "CalloutCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/callouts",
    "public": false,
    "response": "CalloutCreateResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CALLOUT_LIST": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "poolAddress",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/pools/{poolAddress}/callouts",
    "public": false,
    "response": "CalloutListResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CANDLES": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "interval",
        "required": true,
        "schema": {
          "enum": [
            "1m",
            "5m",
            "15m",
            "1h",
            "4h",
            "1d",
            "1w"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "from",
        "required": true,
        "schema": {
          "format": "date-time",
          "pattern": "^(?:(?:000[1-9]|00[1-9][0-9]|0[1-9][0-9]{2}|[1-9][0-9]{3})-(?:(?:01|03|05|07|08|10|12)-(?:0[1-9]|[12][0-9]|3[01])|(?:04|06|09|11)-(?:0[1-9]|[12][0-9]|30)|02-(?:0[1-9]|1[0-9]|2[0-8]))|(?:[0-9]{2}(?:0[48]|[2468][048]|[13579][26])|(?:0[48]|[2468][048]|[13579][26])00)-02-29)T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]\\.[0-9]{3}Z$",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "to",
        "required": true,
        "schema": {
          "format": "date-time",
          "pattern": "^(?:(?:000[1-9]|00[1-9][0-9]|0[1-9][0-9]{2}|[1-9][0-9]{3})-(?:(?:01|03|05|07|08|10|12)-(?:0[1-9]|[12][0-9]|3[01])|(?:04|06|09|11)-(?:0[1-9]|[12][0-9]|30)|02-(?:0[1-9]|1[0-9]|2[0-8]))|(?:[0-9]{2}(?:0[48]|[2468][048]|[13579][26])|(?:0[48]|[2468][048]|[13579][26])00)-02-29)T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]\\.[0-9]{3}Z$",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "anyOf": [
            {
              "maxLength": 256,
              "minLength": 1,
              "type": "string"
            },
            {
              "type": "null"
            }
          ]
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 2000,
          "minimum": 1,
          "type": "integer"
        }
      },
      {
        "in": "path",
        "name": "poolAddress",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/pools/{poolAddress}/candles",
    "public": true,
    "response": "CandlesResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CHALLENGE": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ChallengeBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/auth/challenges",
    "public": false,
    "response": "ChallengeResponse",
    "service": "identity",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_COMMENTS_LIST": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      },
      {
        "in": "path",
        "name": "poolAddress",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/pools/{poolAddress}/comments",
    "public": true,
    "response": "CommentsListResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_COMMENT_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "CommentCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "poolAddress",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/pools/{poolAddress}/comments",
    "public": false,
    "response": "CommentCreateResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_COMMENT_DELETE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": true,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "DELETE",
    "parameters": [
      {
        "in": "path",
        "name": "commentId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/comments/{commentId}",
    "public": false,
    "response": null,
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_COMMENT_EDIT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "CommentEditBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PATCH",
    "parameters": [
      {
        "in": "path",
        "name": "commentId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/comments/{commentId}",
    "public": false,
    "response": "CommentEditResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_COMMENT_LIKE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PUT",
    "parameters": [
      {
        "in": "path",
        "name": "commentId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/comments/{commentId}/likes/me",
    "public": false,
    "response": "CommentLikeResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_COMMENT_UNLIKE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": true,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "DELETE",
    "parameters": [
      {
        "in": "path",
        "name": "commentId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/comments/{commentId}/likes/me",
    "public": false,
    "response": null,
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_ACCOUNT_ASSET_GET": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "wallet",
        "required": true,
        "schema": {
          "maxLength": 42,
          "minLength": 42,
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      },
      {
        "in": "path",
        "name": "assetId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/accounts/{wallet}/assets/{assetId}",
    "public": false,
    "response": "ConnectedAccountAsset",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_ACCOUNT_ASSET_PUT": {
    "authenticated": true,
    "binaryBody": true,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "PUT",
    "parameters": [
      {
        "in": "path",
        "name": "wallet",
        "required": true,
        "schema": {
          "maxLength": 42,
          "minLength": 42,
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      },
      {
        "in": "path",
        "name": "assetId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      },
      {
        "in": "header",
        "name": "Idempotency-Key",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/accounts/{wallet}/assets/{assetId}",
    "public": false,
    "response": "ConnectedAccountAsset",
    "service": "media",
    "timeoutMs": 20000,
    "wireMajor": 2
  },
  "IF_CONNECTED_CHAT_CONVERSATIONS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [],
    "path": "/v2/chat/conversations",
    "public": false,
    "response": "ConnectedChatConversationsResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_CHAT_HISTORY": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "conversation_id",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 2048,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/chat/history",
    "public": false,
    "response": "ConnectedFanoutHistoryPage",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_CHAT_READ": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedChatReadBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/chat/read",
    "public": false,
    "response": "ConnectedSuccessResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_CHAT_REVISE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedChatReviseBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/chat/revise",
    "public": false,
    "response": "ConnectedFanoutDurableMessage",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_DEVICES": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [],
    "path": "/v1/me/devices",
    "public": false,
    "response": "ConnectedDevicesResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 1
  },
  "IF_CONNECTED_DISCOVERY_ALERT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedAlertPutBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PUT",
    "parameters": [
      {
        "in": "path",
        "name": "id",
        "required": true,
        "schema": {
          "maxLength": 160,
          "minLength": 1,
          "type": "string"
        }
      }
    ],
    "path": "/v2/discovery/alerts/{id}",
    "public": false,
    "response": "ConnectedDiscoveryAlertRule",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_DISCOVERY_CALLOUT_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedCalloutCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/discovery/callouts",
    "public": false,
    "response": "ConnectedDiscoveryCallout",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_DISCOVERY_CALLOUT_EDIT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedCalloutEditBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PATCH",
    "parameters": [
      {
        "in": "path",
        "name": "id",
        "required": true,
        "schema": {
          "maxLength": 160,
          "minLength": 1,
          "type": "string"
        }
      }
    ],
    "path": "/v2/discovery/callouts/{id}",
    "public": false,
    "response": "ConnectedDiscoveryCallout",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_DISCOVERY_LEADERBOARDS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "start",
        "required": true,
        "schema": {
          "format": "date-time",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "end",
        "required": true,
        "schema": {
          "format": "date-time",
          "type": "string"
        }
      }
    ],
    "path": "/v2/discovery/leaderboards",
    "public": false,
    "response": "ConnectedDiscoveryLeaderboardV2",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_DISCOVERY_SQUAD_ACCEPT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedSquadAcceptBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "id",
        "required": true,
        "schema": {
          "maxLength": 160,
          "minLength": 1,
          "type": "string"
        }
      }
    ],
    "path": "/v2/discovery/squads/{id}/accept",
    "public": false,
    "response": "ConnectedDiscoveryMembership",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_DISCOVERY_SQUAD_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedSquadCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/discovery/squads",
    "public": false,
    "response": "ConnectedDiscoverySquad",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_DISCOVERY_SQUAD_INVITE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedSquadInviteBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "id",
        "required": true,
        "schema": {
          "maxLength": 160,
          "minLength": 1,
          "type": "string"
        }
      }
    ],
    "path": "/v2/discovery/squads/{id}/invitations",
    "public": false,
    "response": "ConnectedInvitedResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_DISCOVERY_SQUAD_MEMBER": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedSquadMemberBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PATCH",
    "parameters": [
      {
        "in": "path",
        "name": "id",
        "required": true,
        "schema": {
          "maxLength": 160,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "path",
        "name": "account",
        "required": true,
        "schema": {
          "maxLength": 160,
          "minLength": 1,
          "type": "string"
        }
      }
    ],
    "path": "/v2/discovery/squads/{id}/members/{account}",
    "public": false,
    "response": "ConnectedUpdatedResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_DISCOVERY_SQUAD_PATCH": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedSquadPatchBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PATCH",
    "parameters": [
      {
        "in": "path",
        "name": "id",
        "required": true,
        "schema": {
          "maxLength": 160,
          "minLength": 1,
          "type": "string"
        }
      }
    ],
    "path": "/v2/discovery/squads/{id}",
    "public": false,
    "response": "ConnectedDiscoverySquad",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_DISCOVERY_SQUAD_TRANSFER": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedSquadTransferBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "id",
        "required": true,
        "schema": {
          "maxLength": 160,
          "minLength": 1,
          "type": "string"
        }
      }
    ],
    "path": "/v2/discovery/squads/{id}/transfer",
    "public": false,
    "response": "ConnectedAcceptedResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_EXPANDED_SEARCH": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "q",
        "required": true,
        "schema": {
          "maxLength": 256,
          "minLength": 2,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "type",
        "required": true,
        "schema": {
          "enum": [
            "pool",
            "token",
            "profile"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "maximum": 25,
          "minimum": 1,
          "type": "integer"
        }
      },
      {
        "in": "query",
        "name": "sort",
        "required": false,
        "schema": {
          "enum": [
            "address"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "filter",
        "required": false,
        "schema": {
          "maxLength": 2048,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 2048,
          "type": "string"
        }
      }
    ],
    "path": "/v2/search/expanded",
    "public": false,
    "response": "ConnectedExpandedSearchResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_EXTENDED_PREFERENCES_GET": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [],
    "path": "/v3/me/preferences",
    "public": false,
    "response": "ConnectedExtendedPreferencesResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_EXTENDED_PREFERENCES_PATCH": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedExtendedPreferencesBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PATCH",
    "parameters": [],
    "path": "/v3/me/preferences",
    "public": false,
    "response": "ConnectedExtendedPreferencesResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_EXTENDED_PROFILE_GET": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "wallet",
        "required": true,
        "schema": {
          "maxLength": 42,
          "minLength": 42,
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v3/profiles/{wallet}",
    "public": false,
    "response": "ConnectedExtendedProfileResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_EXTENDED_PROFILE_PATCH": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedExtendedProfileBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PATCH",
    "parameters": [
      {
        "in": "path",
        "name": "wallet",
        "required": true,
        "schema": {
          "maxLength": 42,
          "minLength": 42,
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v3/profiles/{wallet}",
    "public": false,
    "response": "ConnectedExtendedProfileResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_FEED": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "order",
        "required": false,
        "schema": {
          "enum": [
            "chronological",
            "relevance"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 2048,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "maximum": 50,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/discovery/feed",
    "public": false,
    "response": "ConnectedDiscoveryFeedPage",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_FEED_DETAIL": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "id",
        "required": true,
        "schema": {
          "maxLength": 160,
          "minLength": 1,
          "type": "string"
        }
      }
    ],
    "path": "/v2/discovery/feed/{id}",
    "public": false,
    "response": "ConnectedDiscoveryFeedCard",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_NETWORK_AUTHORITY": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [],
    "path": "/v2/network/authority",
    "public": false,
    "response": "ConnectedNetworkAuthorityResponse",
    "service": "api",
    "timeoutMs": 10000,
    "wireMajor": 2
  },
  "IF_CONNECTED_NOTIFICATION_DELETE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "DELETE",
    "parameters": [
      {
        "in": "path",
        "name": "id",
        "required": true,
        "schema": {
          "maxLength": 64,
          "minLength": 64,
          "pattern": "^[0-9a-f]{64}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/discovery/notifications/{id}",
    "public": false,
    "response": "ConnectedDeletedResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_NOTIFICATION_LIST": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [],
    "path": "/v2/discovery/notifications",
    "public": false,
    "response": "ConnectedNotificationListResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_NOTIFICATION_READ": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedBooleanReadBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PATCH",
    "parameters": [
      {
        "in": "path",
        "name": "id",
        "required": true,
        "schema": {
          "maxLength": 64,
          "minLength": 64,
          "pattern": "^[0-9a-f]{64}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/discovery/notifications/{id}",
    "public": false,
    "response": "ConnectedReadResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_PNL_INTERVAL_V3": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "from",
        "required": true,
        "schema": {
          "format": "date-time",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "to",
        "required": true,
        "schema": {
          "format": "date-time",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "interval",
        "required": false,
        "schema": {
          "enum": [
            "1h",
            "1d"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "maximum": 1000,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v3/me/pnl/history",
    "public": false,
    "response": "ConnectedL16IntervalSeries",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_PORTFOLIO_CARD_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedPortfolioCardBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "header",
        "name": "Idempotency-Key",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/portfolio/cards",
    "public": false,
    "response": "ConnectedL17StoredCard",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_PORTFOLIO_CARD_IMAGE": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": true,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "cardId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/portfolio/cards/{cardId}/image.png",
    "public": false,
    "response": null,
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_PORTFOLIO_CARD_REVOKE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "DELETE",
    "parameters": [
      {
        "in": "path",
        "name": "cardId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      },
      {
        "in": "header",
        "name": "Idempotency-Key",
        "required": false,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/portfolio/cards/{cardId}",
    "public": false,
    "response": "ConnectedStateResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_PORTFOLIO_CARD_STATUS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "cardId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/portfolio/cards/{cardId}",
    "public": false,
    "response": "ConnectedL17StoredCard",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_PORTFOLIO_DOWNLOAD": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": true,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "jobId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/portfolio/exports/{jobId}/download",
    "public": false,
    "response": null,
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_PORTFOLIO_EXPORT_CANCEL": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "DELETE",
    "parameters": [
      {
        "in": "path",
        "name": "jobId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      },
      {
        "in": "header",
        "name": "Idempotency-Key",
        "required": false,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/portfolio/exports/{jobId}",
    "public": false,
    "response": "ConnectedStateResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_PORTFOLIO_EXPORT_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedPortfolioExportBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "header",
        "name": "Idempotency-Key",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/portfolio/exports",
    "public": false,
    "response": "ConnectedL17ExportJob",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_PORTFOLIO_EXPORT_STATUS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "jobId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/portfolio/exports/{jobId}",
    "public": false,
    "response": "ConnectedL17ExportJob",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_PORTFOLIO_INTERVAL_V3": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "from",
        "required": true,
        "schema": {
          "format": "date-time",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "to",
        "required": true,
        "schema": {
          "format": "date-time",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "interval",
        "required": false,
        "schema": {
          "enum": [
            "1h",
            "1d"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "maximum": 1000,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v3/me/portfolio/history",
    "public": false,
    "response": "ConnectedL16IntervalSeries",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_PORTFOLIO_V3": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 2048,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v3/me/portfolio",
    "public": false,
    "response": "ConnectedPortfolioV3Response",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_PREFLIGHT_CLAIM": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedPreflightClaimBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/preflight/claim",
    "public": false,
    "response": "ConnectedCapabilitiesPreflight",
    "service": "api",
    "timeoutMs": 10000,
    "wireMajor": 2
  },
  "IF_CONNECTED_PREFLIGHT_CREATE": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedPreflightCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/preflight/create",
    "public": false,
    "response": "ConnectedCapabilitiesPreflight",
    "service": "api",
    "timeoutMs": 10000,
    "wireMajor": 2
  },
  "IF_CONNECTED_PREFLIGHT_QUOTE": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedPreflightQuoteBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/preflight/quote",
    "public": false,
    "response": "ConnectedCapabilitiesPreflight",
    "service": "api",
    "timeoutMs": 10000,
    "wireMajor": 2
  },
  "IF_CONNECTED_PREFLIGHT_TRADE": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedPreflightTradeBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/preflight/trade",
    "public": false,
    "response": "ConnectedCapabilitiesPreflight",
    "service": "api",
    "timeoutMs": 10000,
    "wireMajor": 2
  },
  "IF_CONNECTED_PUBLIC_PORTFOLIO_V3": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "wallet",
        "required": true,
        "schema": {
          "maxLength": 42,
          "minLength": 42,
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 2048,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v3/wallets/{wallet}/portfolio",
    "public": false,
    "response": "ConnectedPortfolioV3Response",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_REFERRAL_BIND": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedReferralBindBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "query",
        "name": "campaign",
        "required": false,
        "schema": {
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "policyVersion",
        "required": false,
        "schema": {
          "type": "string"
        }
      },
      {
        "in": "header",
        "name": "Idempotency-Key",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/referrals:bind",
    "public": false,
    "response": "ConnectedL18Binding",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_REFERRAL_CLICK": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedReferralClickBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "query",
        "name": "campaign",
        "required": false,
        "schema": {
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "policyVersion",
        "required": false,
        "schema": {
          "type": "string"
        }
      },
      {
        "in": "header",
        "name": "Idempotency-Key",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/referrals:click",
    "public": false,
    "response": "ConnectedL18ReferralClick",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_REFERRAL_CODE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedReferralCodeBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "query",
        "name": "campaign",
        "required": false,
        "schema": {
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "policyVersion",
        "required": false,
        "schema": {
          "type": "string"
        }
      },
      {
        "in": "header",
        "name": "Idempotency-Key",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/referrals:issue-code",
    "public": false,
    "response": "ConnectedL18ReferralCode",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_REFERRAL_CONSENT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedReferralConsentBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "query",
        "name": "campaign",
        "required": false,
        "schema": {
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "policyVersion",
        "required": false,
        "schema": {
          "type": "string"
        }
      },
      {
        "in": "header",
        "name": "Idempotency-Key",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/referrals:consent",
    "public": false,
    "response": "ConnectedL18ReferralConsent",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_REFERRAL_DASHBOARD": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "campaign",
        "required": false,
        "schema": {
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "policyVersion",
        "required": false,
        "schema": {
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/referrals",
    "public": false,
    "response": "ConnectedL18ReferralDashboard",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_SAFETY_APPEAL": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedSafetyAppealBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/safety/appeals",
    "public": false,
    "response": "ConnectedSafetyAppealOutcome",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_SAFETY_APPEAL_DETAIL": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "appealId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/safety/appeals/{appealId}",
    "public": false,
    "response": "ConnectedSafetyAppealOutcome",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_SAFETY_BAN": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedSafetyBanBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "caseId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/safety/cases/{caseId}/bans",
    "public": false,
    "response": "ConnectedSafetyBanResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_SAFETY_CASES": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "state",
        "required": false,
        "schema": {
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "after",
        "required": false,
        "schema": {
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/safety/cases",
    "public": false,
    "response": "ConnectedSafetyCasesResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_SAFETY_REPORT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedSafetyReportBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/safety/reports",
    "public": false,
    "response": "ConnectedSafetyCase",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_SAFETY_RESTORE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedSafetyRestoreCommand",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "caseId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/safety/cases/{caseId}/restore",
    "public": false,
    "response": "ConnectedSafetyVisibility",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_SAFETY_REVERSE_BAN": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedSafetyReversalCommand",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "appealId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/safety/appeals/{appealId}/reverse-ban",
    "public": false,
    "response": "ConnectedOkResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_SAFETY_TRANSITION": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedSafetyTransitionBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PATCH",
    "parameters": [
      {
        "in": "path",
        "name": "caseId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/safety/cases/{caseId}",
    "public": false,
    "response": "ConnectedRevisionResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_SHARE_V3_IMAGE": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": true,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "shareId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v3/portfolio/shares/{shareId}/image.png",
    "public": false,
    "response": null,
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_SHARE_V3_PREVIEW": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedL17ShareSelectionV3",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v3/me/portfolio/shares/preview",
    "public": false,
    "response": "ConnectedL17SharePreviewV3",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_SHARE_V3_PUBLIC": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "shareId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v3/portfolio/shares/{shareId}",
    "public": false,
    "response": "ConnectedL17PublicShareV3",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_SHARE_V3_PUBLISH": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedL17SharePublishV3",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "header",
        "name": "Idempotency-Key",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v3/me/portfolio/shares",
    "public": false,
    "response": "ConnectedL17OwnerShareV3",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_SHARE_V3_REVOKE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "DELETE",
    "parameters": [
      {
        "in": "path",
        "name": "shareId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v3/me/portfolio/shares/{shareId}",
    "public": false,
    "response": "ConnectedL17OwnerShareV3",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_SHARE_V3_STATUS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "shareId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v3/me/portfolio/shares/{shareId}",
    "public": false,
    "response": "ConnectedL17OwnerShareV3",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_TOKEN_ASSET_GET": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "tokenAddress",
        "required": true,
        "schema": {
          "maxLength": 42,
          "minLength": 42,
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      },
      {
        "in": "path",
        "name": "assetId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/tokens/{tokenAddress}/assets/{assetId}",
    "public": false,
    "response": "ConnectedAssetResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_TOKEN_ASSET_PUT": {
    "authenticated": true,
    "binaryBody": true,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "PUT",
    "parameters": [
      {
        "in": "path",
        "name": "tokenAddress",
        "required": true,
        "schema": {
          "maxLength": 42,
          "minLength": 42,
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      },
      {
        "in": "path",
        "name": "assetId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      },
      {
        "in": "header",
        "name": "Idempotency-Key",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/tokens/{tokenAddress}/assets/{assetId}",
    "public": false,
    "response": "ConnectedAssetResponse",
    "service": "media",
    "timeoutMs": 20000,
    "wireMajor": 2
  },
  "IF_CONNECTED_TOKEN_MEDIA": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "tokenAddress",
        "required": true,
        "schema": {
          "maxLength": 42,
          "minLength": 42,
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/tokens/{tokenAddress}/media",
    "public": false,
    "response": "ConnectedTokenMediaResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_VOICE_END": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedVoiceExpectedBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}/end",
    "public": false,
    "response": "ConnectedVoiceRoomResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_VOICE_LEAVE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}/leave",
    "public": false,
    "response": "ConnectedOkResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_VOICE_PROMPT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}/prompt",
    "public": false,
    "response": "ConnectedVoicePromptResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_VOICE_RECORDING_CONSENT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedVoiceRecordingConsentBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}/recording/consent",
    "public": false,
    "response": "ConnectedOkResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_VOICE_RECORDING_GET": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}/recording",
    "public": false,
    "response": "ConnectedVoiceRecording",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_VOICE_RECORDING_POST": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}/recording",
    "public": false,
    "response": "ConnectedVoiceRecording",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_VOICE_RECORDING_START": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedVoiceExpectedBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}/recording/start",
    "public": false,
    "response": "ConnectedVoiceRecording",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_VOICE_RECORDING_STOP": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedVoiceExpectedBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}/recording/stop",
    "public": false,
    "response": "ConnectedVoiceRecording",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_VOICE_SPEAKERS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedVoiceSpeakerBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}/speakers",
    "public": false,
    "response": "ConnectedOkResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_VOICE_SPEAKER_REQUESTS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedVoiceExpectedBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}/speaker-requests",
    "public": false,
    "response": "ConnectedOkResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_VOICE_START": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedVoiceExpectedBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}/start",
    "public": false,
    "response": "ConnectedVoiceRoomResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CONNECTED_VOICE_V3_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedVoiceCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v3/voice-rooms",
    "public": false,
    "response": "ConnectedVoiceRoomResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_VOICE_V3_MODERATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedVoiceModerationBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v3/voice-rooms/{roomId}/moderation-actions",
    "public": false,
    "response": "ConnectedOkResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CONNECTED_VOICE_V3_TICKET": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ConnectedVoiceTicketBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v3/voice-rooms/{roomId}/tickets",
    "public": false,
    "response": "ConnectedVoiceTicketResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 3
  },
  "IF_CUP_CURRENT": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [],
    "path": "/v2/cups/current",
    "public": true,
    "response": "CupCurrentResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CUP_GET": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "cupId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/cups/{cupId}",
    "public": true,
    "response": "CupGetResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CUP_RESULTS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "cupId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/cups/{cupId}/results",
    "public": true,
    "response": "CupResultsResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_CUP_STANDINGS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "cupId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/cups/{cupId}/standings",
    "public": true,
    "response": "CupStandingsResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_DEPLOYMENT": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [],
    "path": "/v2/network/deployment",
    "public": true,
    "response": "DeploymentResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_DEPOSIT_INTENT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "DepositIntentBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/me/funding/deposit-intents",
    "public": false,
    "response": "DepositIntentResponse",
    "service": "api",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_DEVICE_REGISTER": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "DeviceRegisterBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "PUT",
    "parameters": [
      {
        "in": "path",
        "name": "deviceId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/devices/{deviceId}",
    "public": false,
    "response": "DeviceRegisterResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_DEVICE_REVOKE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": true,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "DELETE",
    "parameters": [
      {
        "in": "path",
        "name": "deviceId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/devices/{deviceId}",
    "public": false,
    "response": null,
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_DM_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "DmCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/me/conversations",
    "public": false,
    "response": "DmCreateResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_DM_LIST": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/me/conversations",
    "public": false,
    "response": "DmListResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_EVENTS_SSE": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": true,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "explode": false,
        "in": "query",
        "name": "topics",
        "required": true,
        "schema": {
          "items": {
            "maxLength": 256,
            "minLength": 1,
            "type": "string"
          },
          "maxItems": 20,
          "minItems": 1,
          "type": "array",
          "uniqueItems": true
        },
        "style": "form"
      },
      {
        "in": "header",
        "name": "Last-Event-ID",
        "required": false,
        "schema": {
          "$ref": "#/$defs/OpaqueResumeCursor"
        }
      },
      {
        "in": "header",
        "name": "Accept",
        "required": true,
        "schema": {
          "const": "text/event-stream",
          "type": "string"
        }
      }
    ],
    "path": "/v2/events/stream",
    "public": false,
    "response": null,
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_FOLLOW": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PUT",
    "parameters": [
      {
        "in": "path",
        "name": "wallet",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/following/{wallet}",
    "public": false,
    "response": "FollowResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_FOLLOWERS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      },
      {
        "in": "path",
        "name": "wallet",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/profiles/{wallet}/followers",
    "public": true,
    "response": "FollowersResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_FOLLOW_STATS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "wallet",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/profiles/{wallet}/follow-stats",
    "public": false,
    "response": "FollowStatsResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_FUNDING_RAILS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "chainId",
        "required": true,
        "schema": {
          "const": "125",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "assetAddress",
        "required": false,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/funding/rails",
    "public": false,
    "response": "FundingRailsResponse",
    "service": "api",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_FUNDING_STATUS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "fundingId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/funding/{fundingId}",
    "public": false,
    "response": "FundingStatusResponse",
    "service": "api",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_HOLDERS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      },
      {
        "in": "path",
        "name": "poolAddress",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/pools/{poolAddress}/holders",
    "public": true,
    "response": "HoldersResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_HOLDER_REWARDS_POOLS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "sort",
        "required": false,
        "schema": {
          "enum": [
            "rewards",
            "mcap",
            "holders",
            "age"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/holder-rewards/pools",
    "public": true,
    "response": "HolderRewardsPoolsResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_HOLDER_REWARDS_STATS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [],
    "path": "/v2/holder-rewards/stats",
    "public": true,
    "response": "HolderRewardsStatsResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_LEADERBOARDS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "category",
        "required": true,
        "schema": {
          "enum": [
            "creator",
            "trader",
            "volume"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "window",
        "required": true,
        "schema": {
          "enum": [
            "24h",
            "7d",
            "30d"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/leaderboards/{category}",
    "public": false,
    "response": "LeaderboardsResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_LIVE_WEBHOOK": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "LiveWebhookBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/provider-webhooks/livepeer",
    "public": false,
    "response": "LiveWebhookResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_LOGIN": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "LoginBody",
    "cookieSession": true,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/auth/sessions",
    "public": false,
    "response": "LoginResponse",
    "service": "identity",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_LOGOUT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": true,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "DELETE",
    "parameters": [
      {
        "in": "path",
        "name": "sessionId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/auth/sessions/{sessionId}",
    "public": false,
    "response": null,
    "service": "identity",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_MESSAGES_LIST": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/rooms/{roomId}/messages",
    "public": false,
    "response": "MessagesListResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_MESSAGE_SEND": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "MessageSendBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/rooms/{roomId}/messages",
    "public": false,
    "response": "MessageSendResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_METADATA_GET": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "tokenAddress",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/tokens/{tokenAddress}/metadata",
    "public": true,
    "response": "MetadataGetResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_METADATA_PATCH": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "MetadataPatchBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PATCH",
    "parameters": [
      {
        "in": "path",
        "name": "tokenAddress",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/tokens/{tokenAddress}/metadata",
    "public": false,
    "response": "MetadataPatchResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_MODERATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ModerateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/admin/moderation-actions",
    "public": false,
    "response": "ModerateResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_NARRATIVES": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      },
      {
        "in": "query",
        "name": "status",
        "required": true,
        "schema": {
          "const": "active",
          "type": "string"
        }
      }
    ],
    "path": "/v2/narratives",
    "public": false,
    "response": "NarrativesResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_NARRATIVE_FEED": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "narrativeId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/narratives/{narrativeId}/feed",
    "public": false,
    "response": "NarrativeFeedResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_NOTIFICATIONS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "unreadOnly",
        "required": false,
        "schema": {
          "type": "boolean"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/me/notifications",
    "public": false,
    "response": "NotificationsResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_NOTIFICATION_READ": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PUT",
    "parameters": [
      {
        "in": "path",
        "name": "notificationId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/notifications/{notificationId}/read",
    "public": false,
    "response": "NotificationReadResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_OPERATOR_DLQ": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "stream",
        "required": true,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "consumer",
        "required": true,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/operator/dlq",
    "public": false,
    "response": "OperatorDlqResponse",
    "service": "api",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_OPERATOR_HEALTH": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "explode": false,
        "in": "query",
        "name": "services",
        "required": true,
        "schema": {
          "items": {
            "maxLength": 256,
            "minLength": 1,
            "type": "string"
          },
          "maxItems": 20,
          "minItems": 0,
          "type": "array"
        },
        "style": "form"
      }
    ],
    "path": "/v2/operator/health",
    "public": false,
    "response": "OperatorHealthResponse",
    "service": "api",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_OPERATOR_JOB_STATUS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "jobId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/operator/jobs/{jobId}",
    "public": false,
    "response": "OperatorJobStatusResponse",
    "service": "api",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_OPERATOR_METRICS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "metric",
        "required": true,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "service",
        "required": true,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "from",
        "required": true,
        "schema": {
          "format": "date-time",
          "pattern": "^(?:(?:000[1-9]|00[1-9][0-9]|0[1-9][0-9]{2}|[1-9][0-9]{3})-(?:(?:01|03|05|07|08|10|12)-(?:0[1-9]|[12][0-9]|3[01])|(?:04|06|09|11)-(?:0[1-9]|[12][0-9]|30)|02-(?:0[1-9]|1[0-9]|2[0-8]))|(?:[0-9]{2}(?:0[48]|[2468][048]|[13579][26])|(?:0[48]|[2468][048]|[13579][26])00)-02-29)T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]\\.[0-9]{3}Z$",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "to",
        "required": true,
        "schema": {
          "format": "date-time",
          "pattern": "^(?:(?:000[1-9]|00[1-9][0-9]|0[1-9][0-9]{2}|[1-9][0-9]{3})-(?:(?:01|03|05|07|08|10|12)-(?:0[1-9]|[12][0-9]|3[01])|(?:04|06|09|11)-(?:0[1-9]|[12][0-9]|30)|02-(?:0[1-9]|1[0-9]|2[0-8]))|(?:[0-9]{2}(?:0[48]|[2468][048]|[13579][26])|(?:0[48]|[2468][048]|[13579][26])00)-02-29)T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]\\.[0-9]{3}Z$",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "stepSeconds",
        "required": true,
        "schema": {
          "maximum": 3600,
          "minimum": 10,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/operator/metrics/query",
    "public": false,
    "response": "OperatorMetricsResponse",
    "service": "api",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_OPERATOR_REDRIVE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "OperatorRedriveBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/operator/dlq:redrive",
    "public": false,
    "response": "OperatorRedriveResponse",
    "service": "api",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_OPERATOR_REPLAY": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "OperatorReplayBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/operator/replays",
    "public": false,
    "response": "OperatorReplayResponse",
    "service": "api",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_ORDER_CANCEL": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "orderId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/orders/{orderId}:cancel",
    "public": false,
    "response": "OrderCancelResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_ORDER_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "OrderCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/me/orders",
    "public": false,
    "response": "OrderCreateResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_ORDER_LIST": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "status",
        "required": false,
        "schema": {
          "enum": [
            "active",
            "filled",
            "cancelled",
            "failed",
            "expired"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/me/orders",
    "public": false,
    "response": "OrderListResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_PLATFORM": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "window",
        "required": true,
        "schema": {
          "enum": [
            "24h",
            "7d",
            "30d"
          ],
          "type": "string"
        }
      }
    ],
    "path": "/v2/platform/metrics",
    "public": true,
    "response": "PlatformResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_PNL_HISTORY": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "from",
        "required": true,
        "schema": {
          "format": "date-time",
          "pattern": "^(?:(?:000[1-9]|00[1-9][0-9]|0[1-9][0-9]{2}|[1-9][0-9]{3})-(?:(?:01|03|05|07|08|10|12)-(?:0[1-9]|[12][0-9]|3[01])|(?:04|06|09|11)-(?:0[1-9]|[12][0-9]|30)|02-(?:0[1-9]|1[0-9]|2[0-8]))|(?:[0-9]{2}(?:0[48]|[2468][048]|[13579][26])|(?:0[48]|[2468][048]|[13579][26])00)-02-29)T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]\\.[0-9]{3}Z$",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "to",
        "required": true,
        "schema": {
          "format": "date-time",
          "pattern": "^(?:(?:000[1-9]|00[1-9][0-9]|0[1-9][0-9]{2}|[1-9][0-9]{3})-(?:(?:01|03|05|07|08|10|12)-(?:0[1-9]|[12][0-9]|3[01])|(?:04|06|09|11)-(?:0[1-9]|[12][0-9]|30)|02-(?:0[1-9]|1[0-9]|2[0-8]))|(?:[0-9]{2}(?:0[48]|[2468][048]|[13579][26])|(?:0[48]|[2468][048]|[13579][26])00)-02-29)T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]\\.[0-9]{3}Z$",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "interval",
        "required": true,
        "schema": {
          "enum": [
            "1h",
            "1d"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 1000,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/me/portfolio/history",
    "public": false,
    "response": "PnlHistoryResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_POOL_DETAIL": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "poolAddress",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/pools/{poolAddress}",
    "public": true,
    "response": "PoolDetailResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_POOL_LIST": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "sort",
        "required": true,
        "schema": {
          "enum": [
            "new",
            "volume",
            "trending"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/pools",
    "public": true,
    "response": "PoolListResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_PORTFOLIO": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/me/portfolio",
    "public": false,
    "response": "PortfolioResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_PREFERENCES_READ": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [],
    "path": "/v2/me/preferences",
    "public": false,
    "response": "PreferencesReadResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_PREFERENCES_UPDATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "PreferencesUpdateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "PATCH",
    "parameters": [],
    "path": "/v2/me/preferences",
    "public": false,
    "response": "PreferencesUpdateResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_PROFILES_BATCH": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "explode": false,
        "in": "query",
        "name": "wallets",
        "required": true,
        "schema": {
          "items": {
            "pattern": "^0x[0-9a-fA-F]{40}$",
            "type": "string"
          },
          "maxItems": 50,
          "minItems": 1,
          "type": "array",
          "uniqueItems": true
        },
        "style": "form"
      }
    ],
    "path": "/v2/profiles:batch",
    "public": true,
    "response": "ProfilesBatchResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_PROFILE_GET": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "wallet",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/profiles/{wallet}",
    "public": true,
    "response": "ProfileGetResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_PROFILE_PATCH": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ProfilePatchBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PATCH",
    "parameters": [
      {
        "in": "path",
        "name": "wallet",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/profiles/{wallet}",
    "public": false,
    "response": "ProfilePatchResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_QUOTE": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "QuoteBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/quotes",
    "public": false,
    "response": "QuoteResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_RANKINGS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "category",
        "required": true,
        "schema": {
          "enum": [
            "trending",
            "breakout",
            "volume",
            "gainers",
            "losers",
            "movers",
            "unusual",
            "new"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "anyOf": [
            {
              "maxLength": 256,
              "minLength": 1,
              "type": "string"
            },
            {
              "type": "null"
            }
          ]
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/rankings/{category}",
    "public": true,
    "response": "RankingsResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_REFRESH": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": true,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "cookie",
        "name": "sidiora_refresh",
        "required": true,
        "schema": {
          "maxLength": 4096,
          "type": "string"
        }
      },
      {
        "in": "header",
        "name": "X-CSRF-Token",
        "required": true,
        "schema": {
          "maxLength": 256,
          "type": "string"
        }
      },
      {
        "in": "header",
        "name": "Origin",
        "required": true,
        "schema": {
          "format": "uri",
          "maxLength": 2048,
          "pattern": "^https://",
          "type": "string"
        }
      }
    ],
    "path": "/v2/auth/sessions:refresh",
    "public": false,
    "response": "RefreshResponse",
    "service": "identity",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_REPORT_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "ReportCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/reports",
    "public": false,
    "response": "ReportCreateResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_REPORT_LIST": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "state",
        "required": true,
        "schema": {
          "enum": [
            "open",
            "reviewing",
            "resolved",
            "dismissed"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/admin/reports",
    "public": false,
    "response": "ReportListResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_REWARDS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "programId",
        "required": false,
        "schema": {
          "maxLength": 64,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/me/rewards",
    "public": false,
    "response": "RewardsResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_REWARD_CLAIM": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "RewardClaimBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/me/rewards:prepare-claim",
    "public": false,
    "response": "RewardClaimResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_SEARCH": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "q",
        "required": true,
        "schema": {
          "maxLength": 256,
          "minLength": 2,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "type",
        "required": true,
        "schema": {
          "enum": [
            "pool",
            "token",
            "profile"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 25,
          "maximum": 25,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/search",
    "public": true,
    "response": "SearchResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_SESSIONS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/auth/sessions",
    "public": false,
    "response": "SessionsResponse",
    "service": "identity",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_SQUAD_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "SquadCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/squads",
    "public": false,
    "response": "SquadCreateResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_SQUAD_MEMBERSHIP": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "SquadMembershipBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "PUT",
    "parameters": [
      {
        "in": "path",
        "name": "squadId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      },
      {
        "in": "path",
        "name": "accountId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/squads/{squadId}/members/{accountId}",
    "public": false,
    "response": "SquadMembershipResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_SQUAD_READ": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "squadId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/squads/{squadId}",
    "public": false,
    "response": "SquadReadResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_STATS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "poolAddress",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/pools/{poolAddress}/stats",
    "public": true,
    "response": "StatsResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_STATS_BATCH": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "StatsBatchBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/pools/stats:batch",
    "public": false,
    "response": "StatsBatchResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_STREAM_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "StreamCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/streams",
    "public": false,
    "response": "StreamCreateResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_STREAM_END": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "streamId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/streams/{streamId}:end",
    "public": false,
    "response": "StreamEndResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_STREAM_HEARTBEAT": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "StreamHeartbeatBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "streamId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/streams/{streamId}/viewers:heartbeat",
    "public": false,
    "response": "StreamHeartbeatResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_STREAM_INGEST_CREDENTIAL": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "streamId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/streams/{streamId}/ingest-credential",
    "public": false,
    "response": "StreamIngestCredentialResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_STREAM_LIST": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "status",
        "required": false,
        "schema": {
          "const": "live",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "poolAddress",
        "required": false,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/streams",
    "public": true,
    "response": "StreamListResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_STREAM_OPERATION_GET": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "operationId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/streams/operations/{operationId}",
    "public": false,
    "response": "StreamOperationGetResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_STREAM_START": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "StreamStartBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "streamId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/streams/{streamId}:start",
    "public": false,
    "response": "StreamStartResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_STREAM_TICKET_ISSUE": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "StreamTicketIssueBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "header",
        "name": "Origin",
        "required": true,
        "schema": {
          "format": "uri",
          "maxLength": 2048,
          "pattern": "^https://",
          "type": "string"
        }
      },
      {
        "description": "Required when cookie-authenticated; validated against session.",
        "in": "header",
        "name": "X-CSRF-Token",
        "required": false,
        "schema": {
          "maxLength": 256,
          "type": "string"
        }
      }
    ],
    "path": "/v2/stream/tickets",
    "public": false,
    "response": "StreamTicketIssueResponse",
    "service": "identity",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_TOPIC_SNAPSHOT_READ": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "explode": false,
        "in": "query",
        "name": "topics",
        "required": true,
        "schema": {
          "items": {
            "maxLength": 256,
            "minLength": 1,
            "type": "string"
          },
          "maxItems": 20,
          "minItems": 1,
          "type": "array",
          "uniqueItems": true
        },
        "style": "form"
      },
      {
        "in": "query",
        "name": "normalizedFilters",
        "required": false,
        "schema": {
          "$ref": "#/$defs/InputTopicFilters"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "anyOf": [
            {
              "$ref": "#/$defs/OpaqueResumeCursor"
            },
            {
              "type": "null"
            }
          ]
        }
      }
    ],
    "path": "/v2/snapshots",
    "public": false,
    "response": "TopicSnapshotReadResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_TRADES": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "poolAddress",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "side",
        "required": false,
        "schema": {
          "enum": [
            "buy",
            "sell"
          ],
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/pools/{poolAddress}/trades",
    "public": true,
    "response": "TradesResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_TX_STATUS": {
    "authenticated": false,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "transactionHash",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{64}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/transactions/{transactionHash}",
    "public": true,
    "response": "TxStatusResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_UNFOLLOW": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": true,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "DELETE",
    "parameters": [
      {
        "in": "path",
        "name": "wallet",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/following/{wallet}",
    "public": false,
    "response": null,
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_UPLOAD_COMPLETE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "UploadCompleteBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "assetId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/assets/{assetId}:complete",
    "public": false,
    "response": "UploadCompleteResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_UPLOAD_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "UploadCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/assets/uploads",
    "public": false,
    "response": "UploadCreateResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_VOICE_CREATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "VoiceCreateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/voice-rooms",
    "public": false,
    "response": "VoiceCreateResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_VOICE_END": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "VoiceEndBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}:end",
    "public": false,
    "response": "VoiceEndResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_VOICE_JOIN_TICKET": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "VoiceJoinTicketBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}/tickets",
    "public": false,
    "response": "VoiceJoinTicketResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_VOICE_LEAVE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "VoiceLeaveBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}:leave",
    "public": false,
    "response": "VoiceLeaveResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_VOICE_MODERATE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "VoiceModerateBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}/moderation-actions",
    "public": false,
    "response": "VoiceModerateResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_VOICE_READ": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "roomId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/voice-rooms/{roomId}",
    "public": false,
    "response": "VoiceReadResponse",
    "service": "media",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_WALLET_LINK_CHALLENGE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "WalletLinkChallengeBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/me/wallet-links/challenges",
    "public": false,
    "response": "WalletLinkChallengeResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_WALLET_LINK_REMOVE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "WalletLinkRemoveBody",
    "cookieSession": false,
    "emptyResponse": true,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "DELETE",
    "parameters": [
      {
        "in": "path",
        "name": "linkId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/wallet-links/{linkId}",
    "public": false,
    "response": null,
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_WALLET_LINK_VERIFY": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "WalletLinkVerifyBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [
      {
        "in": "path",
        "name": "linkId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/wallet-links/{linkId}:verify",
    "public": false,
    "response": "WalletLinkVerifyResponse",
    "service": "api",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_WATCHLIST_DELETE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": true,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "DELETE",
    "parameters": [
      {
        "in": "path",
        "name": "poolAddress",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/watchlist/{poolAddress}",
    "public": false,
    "response": null,
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_WATCHLIST_GET": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "query",
        "name": "cursor",
        "required": false,
        "schema": {
          "maxLength": 256,
          "minLength": 1,
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "limit",
        "required": false,
        "schema": {
          "default": 50,
          "maximum": 100,
          "minimum": 1,
          "type": "integer"
        }
      }
    ],
    "path": "/v2/me/watchlist",
    "public": false,
    "response": "WatchlistGetResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_WATCHLIST_PUT": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "WatchlistPutBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": false,
    "method": "PUT",
    "parameters": [
      {
        "in": "path",
        "name": "poolAddress",
        "required": true,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{40}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/watchlist/{poolAddress}",
    "public": false,
    "response": "WatchlistPutResponse",
    "service": "media",
    "timeoutMs": 1000,
    "wireMajor": 2
  },
  "IF_WITHDRAW_PREPARE": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": "WithdrawPrepareBody",
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": true,
    "idempotentRead": false,
    "method": "POST",
    "parameters": [],
    "path": "/v2/me/funding/withdrawals:prepare",
    "public": false,
    "response": "WithdrawPrepareResponse",
    "service": "api",
    "timeoutMs": 2500,
    "wireMajor": 2
  },
  "IF_WITHDRAW_STATUS": {
    "authenticated": true,
    "binaryBody": false,
    "binaryResponse": false,
    "body": null,
    "cookieSession": false,
    "emptyResponse": false,
    "idempotencyKey": false,
    "idempotentRead": true,
    "method": "GET",
    "parameters": [
      {
        "in": "path",
        "name": "preparationId",
        "required": true,
        "schema": {
          "format": "uuid",
          "type": "string"
        }
      },
      {
        "in": "query",
        "name": "transactionHash",
        "required": false,
        "schema": {
          "pattern": "^0x[0-9a-fA-F]{64}$",
          "type": "string"
        }
      }
    ],
    "path": "/v2/me/funding/withdrawals/{preparationId}",
    "public": false,
    "response": "WithdrawStatusResponse",
    "service": "api",
    "timeoutMs": 2500,
    "wireMajor": 2
  }
} as const;
