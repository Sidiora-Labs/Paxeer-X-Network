# input — Directory Index

## Directory Responsibility

Input components and formatters: Input (with Input.token, Input.tooltip), TextField, InputHelpText, InputAdditional, and inputFormatter (number, currency, identifier, etc.). Extends include QuantityInput.

## Files

| File                                                       | Language   | Summary                                           | Link                                                       |
| ---------------------------------------------------------- | ---------- | ------------------------------------------------- | ---------------------------------------------------------- |
| baseInput.tsx                                              | TSX        | Base input component                              | [baseInput.md](baseInput.md)                               |
| input.tsx                                                  | TSX        | Input and inputVariants                           | [input.md](input.md)                                       |
| input.tooltip.tsx                                          | TSX        | InputWithTooltip                                  | [input.tooltip.md](input.tooltip.md)                       |
| inputAdditional.tsx                                        | TSX        | InputAdditional                                   | [inputAdditional.md](inputAdditional.md)                   |
| inputHelpText.tsx                                          | TSX        | InputHelpText                                     | [inputHelpText.md](inputHelpText.md)                       |
| textField.tsx                                              | TSX        | TextField                                         | [textField.md](textField.md)                               |
| textarea.tsx                                               | TSX        | Textarea component                                | [textarea.md](textarea.md)                                 |
| prefix.tsx                                                 | TSX        | Input prefix                                      | [prefix.md](prefix.md)                                     |
| suffix.tsx                                                 | TSX        | Input suffix                                      | [suffix.md](suffix.md)                                     |
| index.ts                                                   | TypeScript | Re-exports Input, inputFormatter, TextField, etc. | [index.md](index.md)                                       |
| [extends/quantity.tsx](extends/quantity.md)                | TSX        | QuantityInput (Input.token)                       | [extends/quantity.md](extends/quantity.md)                 |
| [formatter/index.ts](formatter/index.md)                   | TypeScript | Formatter barrel                                  | [formatter/index.md](formatter/index.md)                   |
| [formatter/inputFormatter.ts](formatter/inputFormatter.md) | TypeScript | InputFormatter interface                          | [formatter/inputFormatter.md](formatter/inputFormatter.md) |
| [formatter/number.ts](formatter/number.md)                 | TypeScript | Number formatter                                  | [formatter/number.md](formatter/number.md)                 |
| [formatter/currency.ts](formatter/currency.md)             | TypeScript | Currency formatter                                | [formatter/currency.md](formatter/currency.md)             |
| [formatter/identifier.ts](formatter/identifier.md)         | TypeScript | Identifier formatter                              | [formatter/identifier.md](formatter/identifier.md)         |
| Other formatter/\*.ts                                      | TypeScript | decimalPoint, dp, range, regex                    | formatter/\*.md                                            |

## Key Entities

| Entity         | File                 | Role                                                                      |
| -------------- | -------------------- | ------------------------------------------------------------------------- |
| Input          | input.tsx, index.ts  | Main input; Input.token = QuantityInput, Input.tooltip = InputWithTooltip |
| inputFormatter | formatter/           | Format/parse for number, currency, identifier                             |
| TextField      | textField.tsx        | Text field component                                                      |
| QuantityInput  | extends/quantity.tsx | Quantity input (Input.token)                                              |
