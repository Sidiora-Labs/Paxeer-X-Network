# availableToClaim

## Overview

“Available to claim” block: ORDER and esORDER remaining amounts (lifetime/pending minus claimed) and a “Claim” link that opens the app trading rewards page in a new tab.

## Files

| File | Language | Description |
|------|----------|-------------|
| [availableToClaim.script](availableToClaim.script.md) | TSX | `useAvailableScript`, remaining reward calculation, and goToClaim. |
| [availableToClaim.ui](availableToClaim.ui.md) | TSX | `AvailableToClaim` UI with two stat cards. |
| [availableToClaim.widget](availableToClaim.widget.md) | TSX | `AvailableToClaimWidget` that wires script to UI. |
