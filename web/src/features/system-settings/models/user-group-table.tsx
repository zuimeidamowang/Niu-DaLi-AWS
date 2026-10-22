/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Plus, Search, Trash2, X } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from '@/components/ui/input-group'

import { safeJsonParse } from '../utils/json-parser'

type UserGroupRow = {
  id: string
  isDefault: boolean
  name: string
  topupRatio: string
  description: string
}

type UserGroupTableProps = {
  userGroups: string
  topupGroupRatio: string
  legacyUserGroupNames: string[]
  onChange: (field: string, value: string) => void
  onValidationChange?: (invalid: boolean) => void
}

const sectionCardClassName = 'min-w-0 shadow-none'
const sectionHeaderClassName = 'gap-2 border-b'
const maxUserGroupNameLength = 64

let rowId = 0

function createRowId() {
  rowId += 1
  return `ug_${rowId}`
}

function parseStringMap(value: string): Record<string, string> {
  return safeJsonParse<Record<string, string>>(value, {
    fallback: {},
    silent: true,
  })
}

function parseRatioMap(value: string): Record<string, number> {
  return safeJsonParse<Record<string, number>>(value, {
    fallback: {},
    silent: true,
  })
}

function sourceSignature(
  userGroups: string,
  topupGroupRatio: string,
  legacyUserGroupNames: string[]
) {
  return JSON.stringify({
    userGroups: parseStringMap(userGroups),
    topupGroupRatio: parseRatioMap(topupGroupRatio),
    legacyUserGroupNames: [...legacyUserGroupNames].sort(),
  })
}

function buildRows(
  userGroups: string,
  topupGroupRatio: string,
  legacyUserGroupNames: string[] = []
): UserGroupRow[] {
  const descriptions = parseStringMap(userGroups)
  const ratios = parseRatioMap(topupGroupRatio)
  const names = new Set([
    'default',
    ...Object.keys(descriptions),
    ...legacyUserGroupNames,
  ])

  return [...names].map((name) => ({
    id: createRowId(),
    isDefault: name === 'default',
    name,
    topupRatio: Object.hasOwn(ratios, name) ? String(ratios[name]) : '',
    description: descriptions[name] ?? '',
  }))
}

function rowsHaveValidationErrors(rows: UserGroupRow[]) {
  const names = new Set<string>()
  for (const row of rows) {
    const name = row.name.trim()
    if (
      !name ||
      name === 'auto' ||
      [...name].length > maxUserGroupNameLength ||
      names.has(name)
    ) {
      return true
    }
    names.add(name)

    const ratio = row.topupRatio.trim()
    if (ratio && (!Number.isFinite(Number(ratio)) || Number(ratio) < 0)) {
      return true
    }
  }
  return false
}

function serializeRows(rows: UserGroupRow[]) {
  const userGroups: Record<string, string> = {}
  const topupGroupRatio: Record<string, number> = {}

  for (const row of rows) {
    const name = row.name.trim()
    if (!name) continue
    userGroups[name] = row.description
    const ratio = row.topupRatio.trim()
    if (ratio !== '' && Number.isFinite(Number(ratio))) {
      topupGroupRatio[name] = Number(ratio)
    }
  }

  return {
    UserGroups: JSON.stringify(userGroups, null, 2),
    TopupGroupRatio: JSON.stringify(topupGroupRatio, null, 2),
  }
}

export function UserGroupSettingsTable(props: UserGroupTableProps) {
  const { legacyUserGroupNames, onChange, onValidationChange } = props
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [rows, setRows] = useState(() =>
    buildRows(
      props.userGroups,
      props.topupGroupRatio,
      props.legacyUserGroupNames
    )
  )
  const lastSource = useRef(
    sourceSignature(
      props.userGroups,
      props.topupGroupRatio,
      props.legacyUserGroupNames
    )
  )

  useEffect(() => {
    const signature = sourceSignature(
      props.userGroups,
      props.topupGroupRatio,
      props.legacyUserGroupNames
    )
    if (signature === lastSource.current) return
    lastSource.current = signature
    const nextRows = buildRows(
      props.userGroups,
      props.topupGroupRatio,
      props.legacyUserGroupNames
    )
    setRows(nextRows)
    onValidationChange?.(rowsHaveValidationErrors(nextRows))
  }, [
    props.userGroups,
    props.topupGroupRatio,
    props.legacyUserGroupNames,
    onValidationChange,
  ])

  const emitRows = useCallback(
    (nextRows: UserGroupRow[]) => {
      setRows(nextRows)
      const invalid = rowsHaveValidationErrors(nextRows)
      onValidationChange?.(invalid)
      if (invalid) return
      const serialized = serializeRows(nextRows)
      lastSource.current = sourceSignature(
        serialized.UserGroups,
        serialized.TopupGroupRatio,
        legacyUserGroupNames
      )
      onChange('UserGroups', serialized.UserGroups)
      onChange('TopupGroupRatio', serialized.TopupGroupRatio)
    },
    [legacyUserGroupNames, onChange, onValidationChange]
  )

  const updateRow = useCallback(
    (id: string, field: Exclude<keyof UserGroupRow, 'id'>, value: string) => {
      emitRows(
        rows.map((row) => (row.id === id ? { ...row, [field]: value } : row))
      )
    },
    [emitRows, rows]
  )

  const addRow = useCallback(() => {
    setSearch('')
    const existingNames = new Set(rows.map((row) => row.name))
    let index = 1
    let name = `user_group_${index}`
    while (existingNames.has(name)) {
      index += 1
      name = `user_group_${index}`
    }
    emitRows([
      ...rows,
      {
        id: createRowId(),
        isDefault: false,
        name,
        topupRatio: '',
        description: '',
      },
    ])
  }, [emitRows, rows])

  const removeRow = useCallback(
    (id: string) => emitRows(rows.filter((row) => row.id !== id)),
    [emitRows, rows]
  )

  const duplicateNames = useMemo(() => {
    const counts = new Map<string, number>()
    for (const row of rows) {
      const name = row.name.trim()
      if (!name) continue
      counts.set(name, (counts.get(name) ?? 0) + 1)
    }
    return [...counts.entries()]
      .filter(([, count]) => count > 1)
      .map(([name]) => name)
  }, [rows])
  const hasNegativeTopupRatio = useMemo(
    () =>
      rows.some((row) => {
        const ratio = row.topupRatio.trim()
        return (
          ratio !== '' && (!Number.isFinite(Number(ratio)) || Number(ratio) < 0)
        )
      }),
    [rows]
  )
  const hasReservedName = rows.some((row) => row.name.trim() === 'auto')
  const hasOverlongName = rows.some(
    (row) => [...row.name.trim()].length > maxUserGroupNameLength
  )

  const query = search.trim().toLowerCase()
  const visibleRows = rows.filter(
    (row) =>
      !query ||
      row.name.toLowerCase().includes(query) ||
      row.description.toLowerCase().includes(query)
  )

  return (
    <Card className={sectionCardClassName}>
      <CardHeader className={sectionHeaderClassName}>
        <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
          <div>
            <CardTitle>{t('User groups')}</CardTitle>
            <CardDescription>
              {t(
                'Manage account groups and the top-up ratio applied to their members.'
              )}
            </CardDescription>
          </div>
          <Button onClick={addRow} size='sm' className='sm:self-start'>
            <Plus className='mr-2 h-4 w-4' />
            {t('Add user group')}
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <div className='flex min-w-0 flex-col gap-4'>
          <InputGroup className='max-w-sm'>
            <InputGroupAddon>
              <Search aria-hidden='true' />
            </InputGroupAddon>
            <InputGroupInput
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              aria-label={t('Search groups by name or description')}
              placeholder={t('Search groups by name or description')}
            />
            {search && (
              <InputGroupAddon align='inline-end'>
                <InputGroupButton
                  size='icon-xs'
                  aria-label={t('Clear search')}
                  onClick={() => setSearch('')}
                >
                  <X aria-hidden='true' />
                </InputGroupButton>
              </InputGroupAddon>
            )}
          </InputGroup>
          <StaticDataTable
            tableClassName='min-w-[600px]'
            tableProps={{ 'aria-label': t('User groups') }}
            data={visibleRows}
            getRowKey={(row) => row.id}
            emptyClassName='text-muted-foreground h-20 text-sm'
            emptyContent={
              <EmptyState
                className='min-h-40'
                title={
                  query
                    ? t('No results found')
                    : t('No groups yet. Add a group to get started.')
                }
                action={
                  query ? (
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => setSearch('')}
                    >
                      {t('Clear search')}
                    </Button>
                  ) : undefined
                }
              />
            }
            columns={[
              {
                id: 'user-group',
                header: t('User group name'),
                className: 'min-w-40',
                cell: (row) => (
                  <Input
                    value={row.name}
                    aria-label={t('User group name')}
                    disabled={row.isDefault}
                    maxLength={maxUserGroupNameLength * 2}
                    onChange={(event) =>
                      updateRow(row.id, 'name', event.target.value)
                    }
                    aria-invalid={
                      duplicateNames.includes(row.name.trim()) ||
                      row.name.trim() === 'auto' ||
                      [...row.name.trim()].length > maxUserGroupNameLength
                    }
                  />
                ),
              },
              {
                id: 'topup-ratio',
                header: t('Top-up ratio'),
                className: 'w-32',
                cell: (row) => (
                  <Input
                    type='number'
                    min={0}
                    step={0.0001}
                    value={row.topupRatio}
                    aria-label={t('Top-up ratio')}
                    placeholder={t('Not set')}
                    aria-invalid={
                      row.topupRatio.trim() !== '' &&
                      (!Number.isFinite(Number(row.topupRatio)) ||
                        Number(row.topupRatio) < 0)
                    }
                    onChange={(event) =>
                      updateRow(row.id, 'topupRatio', event.target.value)
                    }
                  />
                ),
              },
              {
                id: 'description',
                header: t('Description'),
                className: 'min-w-56',
                cell: (row) => (
                  <Input
                    value={row.description}
                    aria-label={t('Description')}
                    placeholder={t('Description')}
                    onChange={(event) =>
                      updateRow(row.id, 'description', event.target.value)
                    }
                  />
                ),
              },
              {
                id: 'actions',
                header: t('Actions'),
                className: 'text-right',
                cellClassName: 'text-right',
                cell: (row) => (
                  <Button
                    variant='ghost'
                    size='sm'
                    onClick={() => removeRow(row.id)}
                    disabled={row.isDefault}
                    aria-label={t('Delete {{group}}', { group: row.name })}
                  >
                    <Trash2 className='h-4 w-4' />
                  </Button>
                ),
              },
            ]}
          />
          {duplicateNames.length > 0 && (
            <p className='text-destructive text-sm'>
              {t('Duplicate group names: {{names}}', {
                names: duplicateNames.join(', '),
              })}
            </p>
          )}
          {hasReservedName && (
            <p className='text-destructive text-sm' role='alert'>
              {t('The user group name "auto" is reserved.')}
            </p>
          )}
          {hasOverlongName && (
            <p className='text-destructive text-sm' role='alert'>
              {t('User group names must be 64 characters or fewer.')}
            </p>
          )}
          {hasNegativeTopupRatio && (
            <p className='text-destructive text-sm' role='alert'>
              {t('Top-up ratio must be zero or greater')}
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
