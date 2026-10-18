import { Box, Flex, Text } from "@paxeer-network/ui";

export const PaxeerNetworkChartTooltip = (props: {
  label: string;
  value: string | number;
  unit?: string;
  prefix?: React.ReactNode;
  titleClassName?: string;
  coloring?: boolean;
  dp?: number;
  rm?: number;
}) => {
  const {
    label,
    value,
    prefix,
    unit = "USDC",
    coloring = false,
    dp,
    rm,
  } = props;
  return (
    <Box intensity={600} p={3} r="md">
      <Flex direction={"row"} className={props.titleClassName}>
        {prefix}
        <Text.numeral
          unit={unit}
          as="div"
          size="sm"
          coloring={coloring}
          showIdentifier={coloring}
          unitClassName="oui-text-base-contrast-54 oui-ms-1"
          weight="semibold"
          rm={rm}
          dp={dp}
        >
          {value}
        </Text.numeral>
      </Flex>
      <Text size="2xs" intensity={54} weight="semibold">
        {label}
      </Text>
    </Box>
  );
};
