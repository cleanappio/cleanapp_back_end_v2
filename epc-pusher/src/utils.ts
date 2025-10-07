

export function isTruthyEnv(key: string) {
  let v = process.env[key] || ''
  if (!v.trim()) return false
  if (['0', 'f', 'false'].indexOf(v.toLowerCase()) != -1) {
    return false
  }
  return true
}

