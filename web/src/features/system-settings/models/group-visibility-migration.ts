import { safeJsonParse } from '../utils/json-parser'

type LegacyVisibilityMigrationInput = {
  legacyCompatibility: boolean
  legacyUserGroupNames: string[]
  userGroups: string
  groupRatio: string
  userUsableGroups: string
  groupSpecialUsableGroup: string
}

function parseStringMap(value: string): Record<string, string> {
  return safeJsonParse<Record<string, string>>(value, {
    fallback: {},
    silent: true,
  })
}

function parseNestedStringMap(
  value: string
): Record<string, Record<string, string>> {
  return safeJsonParse<Record<string, Record<string, string>>>(value, {
    fallback: {},
    silent: true,
  })
}

export function migrateLegacyUserGroupVisibility(
  input: LegacyVisibilityMigrationInput
): string | null {
  if (!input.legacyCompatibility) return null

  const channelGroups = parseStringMap(input.groupRatio)
  const configuredUserGroups = parseStringMap(input.userGroups)
  const globallyVisibleGroups = parseStringMap(input.userUsableGroups)
  const rules = parseNestedStringMap(input.groupSpecialUsableGroup)
  let changed = false

  for (const userGroup of input.legacyUserGroupNames) {
    if (!Object.hasOwn(configuredUserGroups, userGroup)) continue

    const userGroupRules = rules[userGroup] ?? {}
    const explicitlyConfiguredSameNameRule = Object.keys(userGroupRules).some(
      (ruleKey) => {
        if (ruleKey.startsWith('+:') || ruleKey.startsWith('-:')) {
          return ruleKey.slice(2) === userGroup
        }
        return ruleKey === userGroup
      }
    )

    if (
      !Object.hasOwn(channelGroups, userGroup) ||
      Object.hasOwn(globallyVisibleGroups, userGroup) ||
      explicitlyConfiguredSameNameRule
    ) {
      continue
    }

    rules[userGroup] = { ...userGroupRules, [`+:${userGroup}`]: '用户分组' }
    changed = true
  }

  return changed ? JSON.stringify(rules, null, 2) : null
}
