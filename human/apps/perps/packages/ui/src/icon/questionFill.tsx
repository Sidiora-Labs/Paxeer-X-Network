import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const QuestionFillIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 0.54, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <path
          fill="currentcolor"
          fillOpacity={opacity}
          d="M12.014 1.953c-5.523 0-10 4.477-10 10s4.477 10 10 10 10-4.477 10-10-4.477-10-10-10Zm0 5a3.006 3.006 0 0 1 3 3c.004 1.11-.516 2.044-1.375 2.906-.18.181-.353.355-.563.532-.099.084-.376.287-.406.312-.422.356-1.05.33-1.406-.094a1.035 1.035 0 0 1 .125-1.437l.375-.313c.168-.14.335-.272.47-.406.518-.522.782-1.022.78-1.5a1 1 0 0 0-1.969-.25c-.14.534-.685.86-1.219.72a1.034 1.034 0 0 1-.718-1.25 2.995 2.995 0 0 1 2.906-2.22Zm0 8a1 1 0 1 1 0 2 1 1 0 0 1 0-2Z"
        />
      </BaseIcon>
    );
  }
);

QuestionFillIcon.displayName = "ExclamationFillIcon";
