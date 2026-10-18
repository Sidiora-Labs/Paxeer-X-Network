# useValidateField

## Overview

Hook for validating a single order-edit field. Uses `convertApiOrderTypeToOrderEntryType`, `useOrderEntity`, and `useOrderEntryFormErrorMsg` to validate and return `error`, `errors`, `getErrorMsg`. Runs validation when `value` or `originValue` change.

## Parameters

| Name | Type | Description |
|------|------|-------------|
| `order` | `API.OrderExt \| API.AlgoOrderExt` | Order being edited. |
| `originValue` | `string` | Original value. |
| `value` | `string` | Current value. |
| `field` | `keyof OrderValidationResult` | Field to validate. |
| `fieldValues` | `Partial<PaxeerNetworkOrder>` | Optional other field values. |

## Returns

`{ error, errors, getErrorMsg }`.
