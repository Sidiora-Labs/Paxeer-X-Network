import { cn, Flex } from "@paxeer-network/ui";

export const FlexCell = (props: {
  children: React.ReactNode;
  className?: string;
}) => {
  return (
    <Flex
      direction={"column"}
      justify={"center"}
      itemAlign={"start"}
      className={cn("oui-text-2xs oui-h-[36px]", props.className)}
    >
      {props.children}
    </Flex>
  );
};
