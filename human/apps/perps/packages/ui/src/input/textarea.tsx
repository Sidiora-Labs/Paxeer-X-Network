import React from "react";

interface TextareaProps {}

const Textarea = React.forwardRef<HTMLTextAreaElement, TextareaProps>(
  (props, ref) => {
    return <textarea ref={ref} {...props} />;
  }
);

Textarea.displayName = "Textarea";

export { Textarea };
