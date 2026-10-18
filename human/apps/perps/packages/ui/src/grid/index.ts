import { Grid as GridBase, gridVariants } from "./grid";
import { Span } from "./span";

type GridType = typeof GridBase & {
  span: typeof Span;
};

const Grid = GridBase as GridType;
Grid.span = Span;

export { Grid, gridVariants };
