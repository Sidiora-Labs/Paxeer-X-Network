import { FC, PropsWithChildren } from "react";

type PropType = PropsWithChildren<
  React.DetailedHTMLProps<
    React.ButtonHTMLAttributes<HTMLButtonElement>,
    HTMLButtonElement
  >
>;

export const PrevButton: FC<PropType> = (props) => {
  const { children, ...restProps } = props;

  return (
    <button {...restProps}>
      <svg
        width="20"
        height="20"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
      >
        <rect
          width="20"
          height="20"
          rx="10"
          fill="rgb(var(--oui-color-base-4))"
        />
        <path
          className="oui-fill-base-contrast-54"
          d="M11.186 5.348a.67.67 0 0 0-.436.27l-2.657 4a.69.69 0 0 0 0 .75l2.657 4a.68.68 0 0 0 .934.188.685.685 0 0 0 .187-.937L9.463 9.993 11.87 6.37a.685.685 0 0 0-.187-.938.65.65 0 0 0-.498-.083"
          fill="currentColor"
        />
      </svg>
    </button>
  );
};

export const NextButton: FC<PropType> = (props) => {
  const { children, ...restProps } = props;

  return (
    <button {...restProps}>
      <svg
        width="20"
        height="20"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
      >
        <rect
          width="20"
          height="20"
          rx="10"
          fill="rgb(var(--oui-color-base-4))"
        />
        <path
          className="oui-fill-base-contrast-54"
          d="M8.777 5.348a.65.65 0 0 0-.498.083.685.685 0 0 0-.187.938L10.5 9.993 8.092 13.62a.685.685 0 0 0 .187.937.68.68 0 0 0 .934-.187l2.657-4a.69.69 0 0 0 0-.75l-2.657-4a.67.67 0 0 0-.436-.271"
          fill="currentColor"
        />
      </svg>
    </button>
  );
};
