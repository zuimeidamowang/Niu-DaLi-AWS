/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { ROLE } from '@/lib/roles'
import type { AuthUser } from '@/stores/auth-store'

import type { ManageUserAction, User } from '../types'

export function isDelegatedUserManager(role?: number): boolean {
  return role === ROLE.USER_MANAGER || role === ROLE.THIRD_LEVEL_USER
}

export function canManageUser(viewer: AuthUser | null, target: User): boolean {
  if (!viewer) return false
  if (viewer.role === ROLE.SUPER_ADMIN) {
    return true
  }
  if (viewer.role >= ROLE.ADMIN) return target.role < viewer.role
  if (target.creator_id !== viewer.id) return false
  if (viewer.role === ROLE.THIRD_LEVEL_USER) {
    return target.role === ROLE.USER || target.role === ROLE.USER_MANAGER
  }
  return viewer.role === ROLE.USER_MANAGER && target.role === ROLE.USER
}

export function canViewUser(viewer: AuthUser | null, target: User): boolean {
  if (!viewer) return false
  return viewer.role >= ROLE.ADMIN || canManageUser(viewer, target)
}

// ============================================================================
// User Action Messages
// ============================================================================

const ACTION_MESSAGES: Record<ManageUserAction, string> = {
  enable: 'User enabled successfully',
  disable: 'User disabled successfully',
  promote: 'User promoted to admin successfully',
  promote_manager: 'User promoted to user manager successfully',
  promote_third_level: 'User promoted to third-level user successfully',
  demote: 'User demoted to regular user successfully',
  delete: 'User deleted successfully',
  add_quota: 'Quota adjusted successfully',
}

/**
 * Get success message for user management action
 */
export function getUserActionMessage(action: ManageUserAction): string {
  return ACTION_MESSAGES[action]
}
