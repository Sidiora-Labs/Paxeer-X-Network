# cancelBtn

## Overview

Cancel button for a single order. Uses `useOrderListContext().onCancelOrder`, `ThrottledButton`, and shows loading state and toast on error.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `order` | `API.Order` | Yes | Order to cancel. |
