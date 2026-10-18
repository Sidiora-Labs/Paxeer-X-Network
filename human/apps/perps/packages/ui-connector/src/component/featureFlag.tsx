import { memo } from "react";
import { FlagKeys, useFeatureFlag } from "@paxeer-network/hooks";

/**
 * Props for the FeatureFlag component
 */
export type FeatureFlagProps = {
  /** Content to render when the feature flag is enabled */
  whenEnabled?: React.ReactNode;
  /** Content to render when the feature flag is disabled */
  whenDisabled?: React.ReactNode;
  /** The feature flag key to check */
  flagKey: FlagKeys;
};

/**
 * FeatureFlag component that conditionally renders content based on feature flag status
 *
 * @example
 * ```tsx
 * // Only show when enabled
 * <FeatureFlag
 *   flagKey={FlagKeys.IsolatedMargin}
 *   whenEnabled={<NewIsolatedMarginView />}
 * />
 *
 * // Only show when disabled
 * <FeatureFlag
 *   flagKey={FlagKeys.IsolatedMargin}
 *   whenDisabled={<OldMarginView />}
 * />
 *
 * // Show different content based on flag status
 * <FeatureFlag
 *   flagKey={FlagKeys.IsolatedMargin}
 *   whenEnabled={<NewView />}
 *   whenDisabled={<OldView />}
 * />
 * ```
 */
const FeatureFlag = memo((props: FeatureFlagProps) => {
  const { whenEnabled, whenDisabled, flagKey } = props;
  const { enabled } = useFeatureFlag(flagKey);

  return enabled ? (whenEnabled ?? null) : (whenDisabled ?? null);
});

FeatureFlag.displayName = "FeatureFlag";

export { FeatureFlag };
