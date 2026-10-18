import React from "react";
import { styled } from "storybook/theming";
import { useTheme } from "./context";
import { Label, StyledInput, Wrap } from "./radius";

const StyledGroupName = styled.h3`
  font-size: 14px;
  color: #999;
  font-weight: 700;
`;

const FONT_SIZES = [
  "3xs",
  "2xs",
  "xs",
  "sm",
  "base",
  "lg",
  "xl",
  "2xl",
  "3xl",
  "4xl",
  "5xl",
  "6xl",
] as const;

// const DEFAULT_SIZES = {
//   "3xs": "0.625rem",
//   "2xs": "0.75rem",
//   xs: "13px",
//   sm: "14px",
//   base: "16px",
//   lg: "18px",
//   xl: "20px",
//   "2xl": "24px",
//   "3xl": "28px",
//   "4xl": "30px",
//   "5xl": "36px",
//   "6xl": "48px",
// };

export const Typography = () => {
  const { theme, setTheme } = useTheme();

  const handleChange = (name: string) => (value: string) => {
    setTheme({ ...theme, [name]: value });
  };

  return (
    <div>
      <StyledGroupName>Font Size</StyledGroupName>
      <Wrap>
        {FONT_SIZES.map((size) => (
          <FontSizeInput
            key={size}
            name={size}
            value={theme[`--oui-font-size-${size}`]}
            onChange={handleChange(`--oui-font-size-${size}`)}
          />
        ))}
      </Wrap>
    </div>
  );
};

const FontSizeInput = ({
  name,
  value,
  onChange,
}: {
  name: string;
  value: string;
  onChange: (value: string) => void;
}) => {
  return (
    <div>
      <Label>{name}</Label>
      <div>
        <StyledInput
          type="text"
          value={value}
          onChange={(event) => onChange(event.target.value)}
        />
      </div>
    </div>
  );
};
