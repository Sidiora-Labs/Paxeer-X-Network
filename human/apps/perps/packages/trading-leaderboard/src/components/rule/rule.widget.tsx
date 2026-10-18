import { useScreen } from "@paxeer-network/ui";
import { useCampaignsScript } from "../campaigns/campaigns.script";
import { CampaignRuleUI } from "./rule";
import { CampaignTermsUI } from "./terms";

export const RuleWidget = () => {
  const state = useCampaignsScript();
  const { isMobile } = useScreen();

  const rulesAndTerms = state.currentCampaign?.rule;

  if (
    state.currentCampaignId === "general" ||
    !state.currentCampaign?.rule_config
  ) {
    return null;
  }

  return (
    <>
      <CampaignRuleUI
        id={state.currentCampaign.rule_url || ""}
        className={isMobile ? "oui-px-3" : ""}
        rules={rulesAndTerms?.rule}
        ruleConfig={rulesAndTerms?.ruleConfig}
        isMobile={isMobile}
      />
      <CampaignTermsUI
        className={isMobile ? "oui-px-3" : ""}
        termsConfig={rulesAndTerms?.terms}
        isMobile={isMobile}
      />
    </>
  );
};
