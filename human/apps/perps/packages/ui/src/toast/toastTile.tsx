import { cn } from "..";
import { Flex } from "../flex";
import { Text } from "../typography";

export const ToastTile = (props: {
  title: string;
  subtitle?: string;
  classNames?: {
    className?: string;
    titleClassName?: string;
    subtitleClassName?: string;
  };
}) => {
  return (
    <Flex
      direction="column"
      itemAlign={"start"}
      className={cn("oui-gap-[2px]", props.classNames?.className)}
    >
      <Text
        size="base"
        intensity={80}
        className={props.classNames?.titleClassName}
      >
        {props.title}
      </Text>
      {(props.subtitle?.length || 0) > 0 && (
        <Text
          size="xs"
          intensity={54}
          className={props.classNames?.subtitleClassName}
        >
          {props.subtitle}
        </Text>
      )}
    </Flex>
  );
};
