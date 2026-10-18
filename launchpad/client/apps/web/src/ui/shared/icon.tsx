interface IconProps {
  name: string;
  size?: number;
  className?: string;
  alt?: string;
}

export default function Icon({ name, size = 20, className = '', alt }: IconProps) {
  return (
    <img
      src={`/icons/${name}`}
      width={size}
      height={size}
      alt={alt || name}
      className={className}
    />
  );
}
