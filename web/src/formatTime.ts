const businessTimezone = 'Asia/Shanghai'

const timestampFormatter = new Intl.DateTimeFormat('sv-SE', {
  timeZone: businessTimezone,
  year: 'numeric', month: '2-digit', day: '2-digit',
  hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23',
})

export function formatBeijingTime(timestamp: string): string {
  return timestampFormatter.format(new Date(timestamp))
}
