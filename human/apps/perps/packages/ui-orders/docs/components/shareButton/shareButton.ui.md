# shareButton.ui

## Overview

Presentational share button: renders a button with `ShareIcon` that calls `props.showModal()` on click (with stopPropagation). Returns null when `sharePnLConfig == null`.

## Exports

### `ShareButton`

`FC<ShareButtonScriptReturn>`. Receives script return type (`showModal`, `iconSize`, `sharePnLConfig`).

## Usage example

Used by `ShareButtonWidget`; typically not used directly.
