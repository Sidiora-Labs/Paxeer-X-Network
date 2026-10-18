export const getOperationStatusColor = (status: string) => {
  if (status === "completed") {
    return "success";
  }
  if (status === "rejected" || status === "failed") {
    return "danger";
  }
  return "primary";
};
