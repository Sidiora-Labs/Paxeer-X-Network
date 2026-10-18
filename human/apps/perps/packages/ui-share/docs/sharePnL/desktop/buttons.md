# buttons

## Overview

Prev/Next buttons for the desktop background carousel. Each renders a circular button with an arrow SVG; they accept standard button HTML attributes (e.g. onClick).

## Components

### PrevButton

ForwardRef component; spreads rest props onto a `<button>`. Renders left-arrow icon.

### NextButton

Same as PrevButton with right-arrow icon.

## Usage example

```tsx
<PrevButton onClick={onPrevButtonClick} />
<NextButton onClick={onNextButtonClick} />
```
