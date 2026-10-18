import { FC } from "react";

export const NotFound: FC<{ position: string }> = (props) => {
  const { position } = props;
  return (
    <div style={{ color: "#ef4444" }}>
      <strong>{`[${position}] `}</strong>
      <span>Not found!</span>
    </div>
  );
};
