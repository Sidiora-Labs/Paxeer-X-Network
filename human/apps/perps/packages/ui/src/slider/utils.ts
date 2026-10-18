function clamp(value: number, [min, max]: [number, number]): number {
  return Math.min(max, Math.max(min, value));
}

export function convertValueToPercentage(
  value: number,
  min: number,
  max: number,
) {
  const maxSteps = max - min;
  if (maxSteps === 0) {
    return clamp(0, [0, 100]);
  }
  const percentPerStep = 100 / maxSteps;
  const percentage = percentPerStep * (value - min);
  return clamp(percentage, [0, 100]);
}

function linearScale(
  input: readonly [number, number],
  output: readonly [number, number],
) {
  return (value: number) => {
    if (input[0] === input[1] || output[0] === output[1]) {
      return output[0];
    }
    const ratio = (output[1] - output[0]) / (input[1] - input[0]);
    return output[0] + ratio * (value - input[0]);
  };
}

export function getThumbInBoundsOffset(
  width: number,
  left: number,
  direction: number,
) {
  const halfWidth = width / 2;
  const halfPercent = 50;
  const offset = linearScale([0, halfPercent], [0, halfWidth]);
  return (halfWidth - offset(left) * direction) * direction;
}
