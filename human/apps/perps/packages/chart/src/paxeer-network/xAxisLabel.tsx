import { useTranslation } from "@paxeer-network/i18n";

export const XAxisLabel = (props: any) => {
  const { x, y, stroke, payload, index } = props;
  const { t } = useTranslation();
  return (
    <g transform={`translate(${x},${y - 6})`}>
      <text
        x={0}
        y={0}
        dy={16}
        textAnchor="end"
        direction="ltr"
        unicodeBidi="plaintext"
        fontSize={10}
        fill={"rgba(var(--oui-color-base-foreground)/0.54)"}
      >
        {index === 0 ? payload.value : t("chart.now")}
      </text>
    </g>
  );
};
