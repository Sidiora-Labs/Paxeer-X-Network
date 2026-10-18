export const Checkbox = (props: {
  size?: number;
  className?: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) => {
  const { size = 16, className } = props;
  return (
    <button
      type="button"
      onClick={(e) => {
        props.onCheckedChange(!props.checked);
      }}
      className={className}
    >
      {props.checked ? (
        <svg
          width={size}
          height={size}
          viewBox="0 0 16 16"
          fill="currentColor"
          xmlns="http://www.w3.org/2000/svg"
          className="oui-fill-base-contrast-80"
        >
          <path
            fillRule="evenodd"
            clipRule="evenodd"
            d="M4.66 1.953A2.667 2.667 0 0 0 1.995 4.62v6.667a2.667 2.667 0 0 0 2.667 2.666h6.666a2.667 2.667 0 0 0 2.667-2.666V4.62a2.667 2.667 0 0 0-2.667-2.667zm6.664 2.922a.8.8 0 0 1 .557-.208c.2 0 .406.063.558.208a.734.734 0 0 1 0 1.063l-5.434 5.179a.826.826 0 0 1-1.115 0l-2.33-2.22a.736.736 0 0 1 0-1.063.827.827 0 0 1 1.117 0l1.77 1.687z"
            fill="currentColor"
          />
        </svg>
      ) : (
        <svg
          width={size}
          height={size}
          viewBox="0 0 16 16"
          fill="currentColor"
          xmlns="http://www.w3.org/2000/svg"
          className="oui-fill-base-contrast-80"
        >
          <path
            d="M4.66 1.953A2.667 2.667 0 0 0 1.995 4.62v6.667a2.667 2.667 0 0 0 2.667 2.666h6.666a2.667 2.667 0 0 0 2.667-2.666V4.62a2.667 2.667 0 0 0-2.667-2.667zm0 1.334h6.667c.737 0 1.334.596 1.334 1.333v6.667c0 .736-.597 1.333-1.334 1.333H4.661a1.333 1.333 0 0 1-1.334-1.333V4.62c0-.737.597-1.333 1.334-1.333"
            fill="currentColor"
          />
        </svg>
      )}
    </button>
  );
};
