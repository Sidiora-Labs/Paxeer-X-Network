import { useCallback, useEffect, useRef, useState, MouseEvent } from "react";

type PopoverStateProps = {
  originValue: string;
  value: string;
  setValue: (value: string) => void;
};

export function usePopoverState(props: PopoverStateProps) {
  const { originValue, value, setValue } = props;

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(false);

  const containerRef = useRef<HTMLDivElement | null>(null);

  const closePopover = useCallback(() => {
    setOpen(false);
    setEditing(false);
  }, []);

  const cancelPopover = useCallback(() => {
    closePopover();
    setValue(originValue);
  }, [originValue]);

  const onClick = useCallback(
    async (event: MouseEvent) => {
      event.stopPropagation();
      event.preventDefault();

      // if value is not changed, then don't open popover
      if (value === originValue) {
        setEditing(false);
        return;
      }

      setOpen(true);
    },
    [value, originValue],
  );

  useEffect(() => {
    const handleClickOutside = (event: any) => {
      if (
        containerRef.current &&
        !containerRef.current.contains(event.target as Node) &&
        !open
      ) {
        cancelPopover();
      }
    };

    document.addEventListener("mousedown", handleClickOutside);

    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
    };
  }, [open, cancelPopover]);

  return {
    value,
    setValue,
    open,
    setOpen,
    editing,
    setEditing,
    containerRef,
    closePopover,
    cancelPopover,
    onClick,
  };
}
