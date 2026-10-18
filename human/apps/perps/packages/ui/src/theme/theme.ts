import { ToastOptions } from "react-hot-toast";

export type Theme = {
  rounded: {};
  gaps: {
    grid: 4;
  };

  components: {
    toast: {} & ToastOptions;
  };
};
