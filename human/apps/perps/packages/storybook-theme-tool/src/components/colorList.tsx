import React from "react";
import { styled } from "storybook/theming";
import { ColorGradientInput, ColorInput } from "./colorInput";

type ColorItem = {
  name: string;
  value: string;
};

type ColorGroup = {
  name: string;
  description: string;
  colors: ColorItem[];
};

type GradientColorGroup = {
  name: string;
  start: string;
  end: string;
};

const StyledColorList = styled.div`
  display: flex;
  flex-direction: row;
  justify-content: start;
  flex-wrap: wrap;
  gap: 10px;
`;

const Header = styled.div`
  margin-bottom: 5px;
  margin-top: 10px;
`;

const StyledGroupName = styled.h3`
  font-size: 14px;
  color: #888;
  font-weight: 700;
`;

const StyledGradientGroupName = styled.h3`
  font-size: 14px;
  color: #888;
  font-weight: 700;
  text-transform: capitalize;
`;

const Description = styled.div`
  font-size: 12px;
  color: #777;
  margin-bottom: 10px;
`;

export const ColorList = ({
  colors,
  onChange,
}: {
  colors: ColorGroup[];
  onChange: (name: string, value: string) => void;
}) => {
  return (
    <div>
      {colors.map((color) => (
        <div key={color.name}>
          <Header>
            <StyledGroupName>{color.name}</StyledGroupName>
            <Description>{color.description}</Description>
          </Header>

          <StyledColorList>
            {color.colors.map((color) => (
              <ColorInput
                key={color.name}
                name={color.name}
                color={color.value}
                onChange={(value) => {
                  onChange(color.name, value);
                }}
              />
            ))}
          </StyledColorList>
        </div>
      ))}
    </div>
  );
};

const GradientGrid = styled.div`
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px 30px;
`;

export const GradientColorList = ({
  colors,
  onChange,
}: {
  colors: GradientColorGroup[];
  onChange: (colors: Record<string, string>) => void;
}) => {
  return (
    <div>
      <Header>
        <StyledGroupName>Gradient Colors</StyledGroupName>
        <Description>
          Gradient colors are used for important components such as the ‘Connect
          Wallet’ button and the ‘Enable Trading’ button. If you don’t need
          gradient colors in your application, you can set both the start and
          stop colors to the same value.
        </Description>
      </Header>
      <GradientGrid>
        {colors.map((color) => (
          <div key={color.name}>
            <Header>
              <StyledGradientGroupName>{color.name}</StyledGradientGroupName>
            </Header>
            <ColorGradientInput
              name={color.name}
              start={color.start}
              end={color.end}
              onChange={(start, end) => {
                onChange({
                  [`--oui-gradient-${color.name}-start`]: start,
                  [`--oui-gradient-${color.name}-end`]: end,
                });
              }}
            />
          </div>
        ))}
      </GradientGrid>
    </div>
  );
};
