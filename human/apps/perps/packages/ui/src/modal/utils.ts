const symModalId = Symbol("modalId");

let uidSeed = 0;

const getUid = () => `__modal_${uidSeed++}`;

export const getModalId = (modal: any): string => {
  if (typeof modal === "string") return modal as string;
  if (!modal[symModalId]) {
    modal[symModalId] = getUid();
  }
  return modal[symModalId];
};
