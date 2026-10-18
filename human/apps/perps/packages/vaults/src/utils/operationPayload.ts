import { OperationType, RoleType } from "../types/vault";

export const getToAccountPayloadType = (
  type: OperationType,
  role: RoleType,
) => {
  if (type === OperationType.DEPOSIT && role === RoleType.LP) {
    return 0;
  }
  if (type === OperationType.WITHDRAWAL && role === RoleType.LP) {
    return 1;
  }
  if (type === OperationType.DEPOSIT && role === RoleType.SP) {
    return 2;
  }
  if (type === OperationType.WITHDRAWAL && role === RoleType.SP) {
    return 3;
  }
  return 1;
};
