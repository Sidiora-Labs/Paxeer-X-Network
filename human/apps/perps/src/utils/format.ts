
export function formatLargeNumber(value: string): string {
    const num = parseFloat(value)
    return Math.round(num).toLocaleString('en-US')
}