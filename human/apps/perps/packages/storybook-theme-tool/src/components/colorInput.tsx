import React from "react";
import { styled } from "storybook/theming";
import "./style.css";

const ColorTextInput = styled.input({
  backgroundColor: "transparent",
  border: "none",
  outline: "none",
  // width: "100px",
  textTransform: "uppercase",
  // height: "100%",
});

const ColorNativeInput = styled.input`
  appearance: none;
  -moz-appearance: none;
  -webkit-appearance: none;
  background: none;
  border: 0;
  cursor: pointer;
  height: 5em;
  width: 5em;
  padding: 0;
  &:focus {
    outline: none;
  }
  &::-webkit-color-swatch-wrapper {
    padding: 0;
  }
  &::-webkit-color-swatch {
    border: none;
  }
`;

export const StyledLabel = styled.div`
  font-size: 12px;
  color: #999;
`;

export const ColorInputContainer = styled.div`
  display: flex;
  flex-direction: column;
  gap: 3px;
`;

export const ColorInput = React.memo<{
  name?: string;
  color: string;
  onChange: (color: string) => void;
}>((props) => {
  const { name, color, onChange } = props;
  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    onChange(e.target.value);
  };
  return (
    <ColorInputContainer>
      <ColorNativeInput type="color" value={color} onChange={handleChange} />
      <div>
        <ColorTextInput type="text" value={color} onChange={handleChange} />
        {!!name && <StyledLabel>{name}</StyledLabel>}
      </div>
    </ColorInputContainer>
  );
});

const ColorGradientGroup = styled.div`
  display: flex;
`;

const ColorGradientItemInput: React.FC<{
  name: string;
  color: string;
  onChange: (color: string) => void;
}> = (props) => {
  const { name, color, onChange } = props;
  return (
    <ColorInputContainer style={{ width: "5em" }}>
      <ColorNativeInput
        type="color"
        value={color}
        onChange={(event) => {
          onChange(event.target.value);
        }}
        style={{ borderRadius: "100%" }}
      />
      <div>
        <ColorTextInput
          type="text"
          value={color}
          onChange={onChange as any}
          style={{ borderRadius: "100%", textAlign: "center", width: "100%" }}
        />
      </div>
    </ColorInputContainer>
  );
};

export const ColorGradientInput: React.FC<{
  name: string;
  start: string;
  end: string;
  onChange: (start: string, end: string) => void;
}> = (props) => {
  const { name, start, end, onChange } = props;
  return (
    <ColorGradientGroup>
      <ColorGradientItemInput
        name={name}
        color={start}
        onChange={(color) => {
          onChange(color, end);
        }}
      />
      <div
        style={{
          width: "100%",
          height: "5em",
          background: `linear-gradient(to right, ${start}, ${end})`,
        }}
      ></div>
      <ColorGradientItemInput
        name={name}
        color={end}
        onChange={(color) => {
          onChange(start, color);
        }}
      />
    </ColorGradientGroup>
  );
};
