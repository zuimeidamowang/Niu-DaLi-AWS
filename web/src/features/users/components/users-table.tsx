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
import { useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import type {
  ExpandedState,
  OnChangeFn,
  Row,
  SortingState,
} from '@tanstack/react-table'
import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTableRow,
  DISABLED_ROW_DESKTOP,
  DISABLED_ROW_MOBILE,
  DataTablePage,
  useDataTable,
} from '@/components/data-table'
import { TableCell, TableRow } from '@/components/ui/table'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { ROLE } from '@/lib/roles'
import { createServerError } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { getManagedUserChildren, getUsers, searchUsers } from '../api'
import {
  USER_STATUS,
  getUserStatusOptions,
  getUserRoleOptions,
  isUserDeleted,
} from '../constants'
import type { GetManagedUserChildrenParams, User, UserSortBy } from '../types'
import { DataTableBulkActions } from './data-table-bulk-actions'
import { useUsersColumns } from './users-columns'
import { useUsers } from './users-provider'

const route = getRouteApi('/_authenticated/users/')

const USER_SORTABLE_COLUMNS = new Set<UserSortBy>([
  'id',
  'username',
  'quota',
  'group',
  'created_at',
  'last_login_at',
])
const EMPTY_USERS: User[] = []

function isDisabledUserRow(user: User) {
  return isUserDeleted(user) || user.status === USER_STATUS.DISABLED
}

function canExpandUser(user: User, viewerRole: number) {
  if (viewerRole < ROLE.ADMIN) return false
  return (
    user.hierarchy_auto_expand === true ||
    user.role === ROLE.THIRD_LEVEL_USER ||
    user.role === ROLE.USER_MANAGER
  )
}

function getUserCategory(user: User | undefined, viewerRole: number) {
  if (!user) return ''
  if (viewerRole < ROLE.ADMIN) {
    return user.role === ROLE.USER_MANAGER ? 'User Manager' : 'User'
  }
  if (user.role === ROLE.SUPER_ADMIN) return 'Root'
  if (user.role === ROLE.ADMIN) return 'Admin'
  if (user.role === ROLE.THIRD_LEVEL_USER) return 'Third-level User'
  if (user.role === ROLE.USER_MANAGER) return 'User managers without a parent'
  return 'Users without a parent'
}

function getOpenUserIds(expanded: ExpandedState) {
  if (expanded === true) return null
  return new Set(
    Object.entries(expanded)
      .filter(([, isOpen]) => isOpen)
      .map(([id]) => id)
  )
}

function managedUserChildrenKey(
  viewerId: number | undefined,
  viewerRole: number | undefined,
  parentId: number,
  params: GetManagedUserChildrenParams,
  refreshTrigger: number
) {
  return [
    'managed-user-children',
    viewerId,
    viewerRole,
    parentId,
    params.keyword,
    params.group,
    params.role,
    params.status,
    params.sort_by,
    params.sort_order,
    refreshTrigger,
  ] as const
}

async function getAllManagedUserChildren(
  parentId: number,
  params: GetManagedUserChildrenParams,
  fallbackMessage: string
) {
  const users: User[] = []
  let total = 0
  let page = 1
  while (page === 1 || users.length < total) {
    const result = await getManagedUserChildren(parentId, {
      ...params,
      p: page,
      page_size: 100,
    })
    if (!result.success) {
      throw createServerError(result, fallbackMessage)
    }
    const items = result.data?.items ?? []
    users.push(...items)
    total = result.data?.total ?? users.length
    if (items.length === 0) break
    page += 1
  }
  return users
}

function collectUsers(rows: User[], childRows: Map<number, User[]>) {
  const collected: User[] = []
  const pending = [...rows]
  const seen = new Set<number>()
  while (pending.length > 0) {
    const user = pending.pop()
    if (!user) continue
    if (seen.has(user.id)) continue
    seen.add(user.id)
    collected.push(user)
    pending.push(...(childRows.get(user.id) ?? []))
  }
  return collected
}

function collectExpandedUserTargets(
  roots: User[],
  viewerRole: number,
  openIds: Set<string> | null,
  queryClient: ReturnType<typeof useQueryClient>,
  viewerId: number | undefined,
  params: GetManagedUserChildrenParams,
  refreshTrigger: number
) {
  const targets: User[] = []
  const children = new Map<number, User[]>()
  const pending = [...roots]
  const seen = new Set<number>()
  while (pending.length > 0) {
    const user = pending.pop()
    if (!user) continue
    if (seen.has(user.id)) continue
    seen.add(user.id)
    const isOpen = openIds === null || openIds.has(String(user.id))
    if (!isOpen || !canExpandUser(user, viewerRole)) continue
    targets.push(user)
    const cached = queryClient.getQueryData<User[]>(
      managedUserChildrenKey(
        viewerId,
        viewerRole,
        user.id,
        params,
        refreshTrigger
      )
    )
    if (cached) {
      children.set(user.id, cached)
      pending.push(...cached)
    }
  }
  return { targets, children }
}

export function UsersTable() {
  const { t } = useTranslation()
  const currentUser = useAuthStore((s) => s.auth.user)
  const queryClient = useQueryClient()
  const isUserManager = currentUser?.role === ROLE.USER_MANAGER
  const isThirdLevelUser = currentUser?.role === ROLE.THIRD_LEVEL_USER
  const columns = useUsersColumns()
  const { refreshTrigger } = useUsers()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const [sorting, setSorting] = useState<SortingState>([])
  const [expanded, setExpanded] = useState<ExpandedState>({})

  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 10 : 20 },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [
      { columnId: 'status', searchKey: 'status', type: 'array' },
      { columnId: 'role', searchKey: 'role', type: 'array' },
      { columnId: 'group', searchKey: 'group', type: 'string' },
    ],
  })
  const statusFilter =
    (columnFilters.find((filter) => filter.id === 'status')?.value as
      | string[]
      | undefined) ?? []
  const roleFilter =
    (columnFilters.find((filter) => filter.id === 'role')?.value as
      | string[]
      | undefined) ?? []
  const groupFilter =
    (columnFilters.find((filter) => filter.id === 'group')?.value as string) ??
    ''
  const hasUnsupportedRoleFilter =
    isUserManager ||
    (isThirdLevelUser &&
      roleFilter.some(
        (role) =>
          role !== String(ROLE.USER) && role !== String(ROLE.USER_MANAGER)
      ))
  const effectiveRoleFilter = hasUnsupportedRoleFilter ? [] : roleFilter
  const effectiveColumnFilters = hasUnsupportedRoleFilter
    ? columnFilters.filter((filter) => filter.id !== 'role')
    : columnFilters
  useEffect(() => {
    if (!hasUnsupportedRoleFilter || roleFilter.length === 0) return
    onColumnFiltersChange((previous) =>
      previous.filter((filter) => filter.id !== 'role')
    )
  }, [hasUnsupportedRoleFilter, roleFilter.length, onColumnFiltersChange])

  const sortParams = useMemo(() => {
    const activeSort = sorting[0]
    if (
      !activeSort ||
      !USER_SORTABLE_COLUMNS.has(activeSort.id as UserSortBy)
    ) {
      return {}
    }

    return {
      sort_by: activeSort.id as UserSortBy,
      sort_order: activeSort.desc ? 'desc' : 'asc',
    } as const
  }, [sorting])

  const handleSortingChange: OnChangeFn<SortingState> = (updater) => {
    setSorting(updater)
    if (pagination.pageIndex > 0) {
      onPaginationChange({ ...pagination, pageIndex: 0 })
    }
  }

  // Fetch data with React Query
  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'users',
      currentUser?.id,
      currentUser?.role,
      pagination.pageIndex + 1,
      pagination.pageSize,
      globalFilter,
      statusFilter,
      effectiveRoleFilter,
      groupFilter,
      sortParams,
      refreshTrigger,
    ],
    queryFn: async () => {
      const hasFilter = globalFilter?.trim()
      const hasColumnFilter =
        statusFilter.length > 0 ||
        effectiveRoleFilter.length > 0 ||
        Boolean(groupFilter)
      const params = {
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
        ...sortParams,
      }

      const result =
        hasFilter || hasColumnFilter
          ? await searchUsers({
              ...params,
              keyword: globalFilter,
              status: statusFilter[0] ?? '',
              role: effectiveRoleFilter[0] ?? '',
              group: groupFilter,
              hierarchy: true,
            })
          : await getUsers({ ...params, hierarchy: true })

      if (!result.success) {
        throw createServerError(
          result,
          t(hasFilter ? 'Failed to search users' : 'Failed to load users')
        )
      }

      return {
        items: result.data?.items || [],
        total: result.data?.total || 0,
      }
    },
    enabled: (currentUser?.role ?? 0) >= ROLE.USER_MANAGER,
    placeholderData: (previousData, previousQuery) =>
      previousQuery?.queryKey[1] === currentUser?.id &&
      previousQuery?.queryKey[2] === currentUser?.role
        ? previousData
        : undefined,
  })

  const users = data?.items ?? EMPTY_USERS
  const statusFilterValue = statusFilter.at(0) ?? ''
  const roleFilterValue = effectiveRoleFilter.at(0) ?? ''
  const childParams = useMemo<GetManagedUserChildrenParams>(
    () => ({
      keyword: globalFilter?.trim() ?? '',
      status: statusFilterValue,
      role: roleFilterValue,
      group: groupFilter,
      ...sortParams,
    }),
    [globalFilter, statusFilterValue, roleFilterValue, groupFilter, sortParams]
  )
  const openIds = useMemo(() => getOpenUserIds(expanded), [expanded])
  const expandableTargets = useMemo(
    () =>
      collectExpandedUserTargets(
        users,
        currentUser?.role ?? 0,
        openIds,
        queryClient,
        currentUser?.id,
        childParams,
        refreshTrigger
      ),
    [
      users,
      currentUser?.role,
      currentUser?.id,
      openIds,
      queryClient,
      childParams,
      refreshTrigger,
    ]
  )
  const childQueries = useQueries({
    queries: expandableTargets.targets.map((parent) => ({
      queryKey: managedUserChildrenKey(
        currentUser?.id,
        currentUser?.role,
        parent.id,
        childParams,
        refreshTrigger
      ),
      queryFn: () =>
        getAllManagedUserChildren(
          parent.id,
          childParams,
          t('Failed to load users')
        ),
      enabled: canExpandUser(parent, currentUser?.role ?? 0),
    })),
  })
  const childQueryByParentId = useMemo(
    () =>
      new Map(
        expandableTargets.targets.map((parent, index) => [
          parent.id,
          childQueries[index],
        ])
      ),
    [expandableTargets.targets, childQueries]
  )
  const childrenByParentId = new Map<number, User[]>()
  const hydrate = (user: User, ancestors = new Set<number>()): User => {
    if (ancestors.has(user.id)) return user
    const nextAncestors = new Set(ancestors).add(user.id)
    const query = childQueryByParentId.get(user.id)
    const children = (query?.data ?? []).map((child) =>
      hydrate(child, nextAncestors)
    )
    childrenByParentId.set(user.id, children)
    return {
      ...user,
      children_loading: query?.isLoading ?? false,
      children_error: query?.isError ?? false,
      children_loaded: query?.data !== undefined,
    }
  }
  const tableUsers = users.map((user) => hydrate(user))
  const visibleUsers = collectUsers(tableUsers, childrenByParentId)
  const autoExpandedIds = visibleUsers
    .filter((user) => user.hierarchy_auto_expand)
    .map((user) => String(user.id))
  const autoExpandedSignature = autoExpandedIds.join(',')
  const autoExpandScope = JSON.stringify([
    currentUser?.id,
    currentUser?.role,
    pagination.pageIndex,
    globalFilter,
    statusFilter,
    effectiveRoleFilter,
    groupFilter,
    sortParams,
    refreshTrigger,
  ])
  const autoExpandedUsers = useRef({ scope: '', ids: new Set<string>() })

  useEffect(() => {
    if (!autoExpandedSignature) return
    const autoExpandedIds = autoExpandedSignature.split(',')
    if (autoExpandedUsers.current.scope !== autoExpandScope) {
      autoExpandedUsers.current = { scope: autoExpandScope, ids: new Set() }
    }
    const newIds = autoExpandedIds.filter(
      (id) => !autoExpandedUsers.current.ids.has(id)
    )
    if (!newIds.length) return
    for (const id of newIds) autoExpandedUsers.current.ids.add(id)
    setExpanded((previous) => {
      if (previous === true) return previous
      const next = { ...previous }
      let changed = false
      for (const id of newIds) {
        if (!next[id]) {
          next[id] = true
          changed = true
        }
      }
      return changed ? next : previous
    })
  }, [autoExpandedSignature, autoExpandScope])
  const roleFilterDef = {
    columnId: 'role',
    title: t('Role'),
    options: getUserRoleOptions(t),
    singleSelect: true,
  }
  let roleFilters = [roleFilterDef]
  if (isUserManager) {
    roleFilters = []
  } else if (isThirdLevelUser) {
    roleFilters = [
      {
        ...roleFilterDef,
        options: roleFilterDef.options.filter(
          (option) =>
            option.value === String(ROLE.USER) ||
            option.value === String(ROLE.USER_MANAGER)
        ),
      },
    ]
  }

  const { table } = useDataTable({
    data: tableUsers,
    columns,
    enableRowSelection: true,
    columnFilters: effectiveColumnFilters,
    globalFilter,
    pagination,
    sorting,
    globalFilterFn: (row, _columnId, filterValue) => {
      const searchValue = String(filterValue).toLowerCase()
      const fields = [
        row.getValue('username'),
        row.original.display_name,
        row.original.email,
      ]
      return fields.some((field) =>
        String(field || '')
          .toLowerCase()
          .includes(searchValue)
      )
    },
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
    onSortingChange: handleSortingChange,
    expanded,
    onExpandedChange: setExpanded,
    getRowId: (user) => String(user.id),
    getSubRows: (user) => childrenByParentId.get(user.id) ?? [],
    getRowCanExpand: (row) =>
      canExpandUser(row.original, currentUser?.role ?? 0),
    withExpandedRowModel: true,
    manualPagination: true,
    manualFiltering: true,
    manualSorting: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No Users Found')}
      emptyDescription={t(
        'No users available. Try adjusting your search or filters.'
      )}
      skeletonKeyPrefix='users-skeleton'
      applyHeaderSize
      toolbarProps={{
        searchPlaceholder: t('Filter by username, name or email...'),
        searchDebounceMs: 500,
        filters: [
          {
            columnId: 'status',
            title: t('Status'),
            options: getUserStatusOptions(t),
            singleSelect: true,
          },
          ...roleFilters,
        ],
      }}
      renderRow={(row) => {
        const previous = users[row.index - 1]
        const category = getUserCategory(row.original, currentUser?.role ?? 0)
        const showCategory =
          row.depth === 0 &&
          (row.index === 0 ||
            getUserCategory(previous, currentUser?.role ?? 0) !== category)
        return (
          <Fragment key={row.id}>
            {showCategory && (
              <TableRow className='bg-muted/40 hover:bg-muted/40 h-9'>
                <TableCell
                  colSpan={row.getVisibleCells().length}
                  className='text-muted-foreground text-xs font-medium'
                >
                  {t(category)}
                </TableCell>
              </TableRow>
            )}
            <DataTableRow
              row={row}
              className={
                isDisabledUserRow(row.original)
                  ? DISABLED_ROW_DESKTOP
                  : undefined
              }
            />
          </Fragment>
        )
      }}
      mobileProps={{
        renderRow: (row: Row<User>, defaultCard) => {
          const previous = users[row.index - 1]
          const category = getUserCategory(row.original, currentUser?.role ?? 0)
          const showCategory =
            row.depth === 0 &&
            (row.index === 0 ||
              getUserCategory(previous, currentUser?.role ?? 0) !== category)
          return (
            <Fragment key={row.id}>
              {showCategory && (
                <div className='bg-muted/40 text-muted-foreground px-3 py-2 text-xs font-medium'>
                  {t(category)}
                </div>
              )}
              {defaultCard}
            </Fragment>
          )
        },
      }}
      getRowClassName={(row, { isMobile }) => {
        const classes: string[] = []
        if (isDisabledUserRow(row.original)) {
          classes.push(isMobile ? DISABLED_ROW_MOBILE : DISABLED_ROW_DESKTOP)
        }
        if (isMobile && row.depth > 0) {
          classes.push('ml-4 border-l-2 border-muted')
        }
        return cn(classes)
      }}
      bulkActions={<DataTableBulkActions table={table} />}
    />
  )
}
