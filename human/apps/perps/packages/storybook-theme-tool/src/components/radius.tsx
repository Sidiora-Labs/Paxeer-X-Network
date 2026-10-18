import React from "react";
import { styled } from "storybook/theming";
import { useTheme } from "./context";

export const Wrap = styled.div`
  display: flex;
  flex-direction: row;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 10px;
`;

const StyledGroupName = styled.h3`
  font-size: 14px;
  color: #999;
  font-weight: 700;
`;

const RADIUS_SIZES = [
  "sm",
  "default",
  "md",
  "lg",
  "xl",
  "2xl",
  "full",
] as const;

export const Radius = () => {
  const { theme, setTheme } = useTheme();

  const handleChange = (name: string) => (value: string) => {
    setTheme({ ...theme, [name]: value });
  };

  return (
    <div>
      <StyledGroupName>Border Radius</StyledGroupName>
      <Wrap>
        {RADIUS_SIZES.map((size) => {
          const suffix = size === "default" ? "" : `-${size}`;
          return (
            <RadiusInput
              key={size}
              name={size}
              value={theme[`--oui-rounded${suffix}`]}
              onChange={handleChange(`--oui-rounded${suffix}`)}
            />
          );
        })}
      </Wrap>
    </div>
  );
};

export const Label = styled.div`
  font-size: 12px;
  color: #999;
  margin-bottom: 5px;
`;

export const StyledInput = styled.input`
  width: 100%;
  border: 1px solid #aaa;
  background: transparent;
  border-radius: 5px;
  padding: 4px 10px;
  font-size: 12px;
`;

const RadiusInput = ({
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
