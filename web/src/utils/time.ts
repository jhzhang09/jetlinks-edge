/**
 * 时间日期格式化工具
 * @author jhzhang
 * @date 2026-06-16
 */

/**
 * 格式化 Go 零时间（0001-）为 '--'，其余为本地日期时间字符串
 * @param value 原始时间字符串
 * @returns 格式化后的字符串
 */
export function formatGoDateTime(value?: string): string {
  return !value || value.startsWith('0001-') ? '--' : new Date(value).toLocaleString()
}

/**
 * 格式化 Go 零时间（0001-）为 '--'，其余为本地时间部分字符串
 * @param value 原始时间字符串
 * @returns 格式化后的字符串
 */
export function formatGoTime(value?: string): string {
  return !value || value.startsWith('0001-') ? '--' : new Date(value).toLocaleTimeString()
}
