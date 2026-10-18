export type SizeType = "xs" | "sm" | "md" | "lg" | "xl";

export type ExcludeXsSizeType = Exclude<SizeType, "xs">;
