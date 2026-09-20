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
import { Minimize2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { useAuthStore } from '@/stores/auth-store'

import { ProjectCreateDialog } from './components/project-create-dialog'
import { ProjectDeleteDialog } from './components/project-delete-dialog'
import { ProjectDrawer } from './components/project-drawer'
import { ProjectRail } from './components/project-rail'
import { ProjectRenameDialog } from './components/project-rename-dialog'
import { RemoteBrowserViewport } from './components/remote-browser-viewport'
import { WebWorkspaceDisabled } from './components/web-workspace-disabled'
import {
  WorkspaceNavigationControls,
  WorkspaceToolbar,
} from './components/workspace-toolbar'
import { WEB_WORKSPACE_HIDDEN_ACTIVITY_INTERVAL_MS } from './constants'
import { useCompactSidebar } from './hooks/use-compact-sidebar'
import { useDesktopViewport } from './hooks/use-desktop-viewport'
import { useDocumentVisibility } from './hooks/use-document-visibility'
import { useElementSize } from './hooks/use-element-size'
import { useImmersiveMode } from './hooks/use-immersive-mode'
import { useProjectRemovalNotice } from './hooks/use-project-removal-notice'
import { useRemoteSurface } from './hooks/use-remote-surface'
import { useWebWorkspaceConfig } from './hooks/use-web-workspace-config'
import { useWebWorkspaceProjects } from './hooks/use-web-workspace-projects'
import { useWebWorkspaceProvider } from './hooks/use-web-workspace-provider'
import {
  useNavigateWebWorkspaceSession,
  useRestartWebWorkspaceSession,
  useStartWebWorkspaceSession,
  useStopWebWorkspaceSession,
  useWebWorkspaceActivity,
  useWebWorkspaceSession,
} from './hooks/use-web-workspace-session'
import { resolveWebWorkspaceAccess } from './lib/access'
import { formatByteSize } from './lib/bytes'
import { resolveConnection } from './lib/connection'
import { classifyWebWorkspaceError } from './lib/errors'
import {
  readLastProjectId,
  rememberLastProjectId,
  resolveAutoOpenProject,
} from './lib/last-project'
import {
  findPresentationProfile,
  remoteScreenSizeForFrame,
} from './lib/presentation'
import { isLiveSessionState } from './lib/session'
import type { WebProject, WebWorkspaceNavigationAction } from './types'

/**
 * Remount key for the per-project dialogs so each dialog starts from the
 * selected project instead of the previous one.
 */
function projectDialogKey(prefix: string, project: WebProject | null): string {
  return [prefix, project ? String(project.id) : 'none'].join('-')
}

/**
 * Web Workspace page.
 *
 * The workspace is the remote browser: the global New API navigation stays,
 * but it is compacted to an icon rail, the project rail is the only project
 * navigation and the toolbar keeps the low-frequency session controls out of
 * the main surface. Every value shown here comes from the real control plane
 * and the real remote stream; there is no placeholder project, no simulated
 * ChatGPT markup and no faked connection state.
 */
export function WebWorkspace() {
  const { t } = useTranslation()
  const configQuery = useWebWorkspaceConfig()
  const isDesktop = useDesktopViewport()
  const access = configQuery.data
    ? resolveWebWorkspaceAccess(configQuery.data)
    : null
  const ready = access === 'ready'
  const immersive = useImmersiveMode()
  useCompactSidebar(ready, immersive.immersive)

  const sessionQuery = useWebWorkspaceSession({ enabled: ready })
  const projectsQuery = useWebWorkspaceProjects(ready)
  const provider = useWebWorkspaceProvider(ready)
  const startMutation = useStartWebWorkspaceSession()
  const stopMutation = useStopWebWorkspaceSession()
  const activityMutation = useWebWorkspaceActivity()
  const isVisible = useDocumentVisibility()
  const restartMutation = useRestartWebWorkspaceSession()
  const navigationMutation = useNavigateWebWorkspaceSession()
  const removal = useProjectRemovalNotice(projectsQuery.data)

  const [selectedProjectId, setSelectedProjectId] = useState<number | null>(
    null
  )
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [renameTarget, setRenameTarget] = useState<WebProject | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<WebProject | null>(null)
  const [projectNavigationPending, setProjectNavigationPending] =
    useState(false)
  const [projectNavigationErrorKey, setProjectNavigationErrorKey] = useState<
    string | null
  >(null)
  const autoOpenAttemptRef = useRef<string | null>(null)
  const navigatedProjectRef = useRef<string | null>(null)
  const firstProjectPromptedRef = useRef(false)
  const userId = useAuthStore((state) => state.auth.user?.id ?? null)

  const session = sessionQuery.data ?? null
  const isLive = isLiveSessionState(session?.state)
  const projectCreationFailed = session?.project_creation?.state === 'FAILED'
  const navigation = session?.navigation ?? null
  const projects = projectsQuery.data ?? []
  const projectsLoaded = !projectsQuery.isPending && !projectsQuery.isError
  const firstProjectRequired = ready && projectsLoaded && projects.length === 0
  const selectedProject =
    projects.find((project) => project.id === selectedProjectId) ?? null
  const suppressRemoteStream =
    projectsQuery.isPending ||
    firstProjectRequired ||
    (isLive &&
      projects.length > 0 &&
      (selectedProjectId === null ||
        projectNavigationPending ||
        Boolean(projectNavigationErrorKey)))
  const isBusy =
    startMutation.isPending ||
    stopMutation.isPending ||
    restartMutation.isPending ||
    navigationMutation.isPending

  const surface = useRemoteSurface({
    sessionId: session?.session_id ?? '',
    // A hidden tab detaches the display stream: no framebuffer is sent while
    // the user cannot see it, and the runtime is kept alive by the activity
    // keep-alive below instead.
    enabled: isDesktop && isLive && isVisible,
  })

  const keepAlive = useRef<() => void>(() => undefined)
  keepAlive.current = () => {
    if (session) activityMutation.mutate(session.session_id)
  }
  useEffect(() => {
    if (isVisible || !isLive) return undefined
    const timer = window.setInterval(() => {
      keepAlive.current()
    }, WEB_WORKSPACE_HIDDEN_ACTIVITY_INTERVAL_MS)
    return () => {
      window.clearInterval(timer)
    }
  }, [isVisible, isLive])

  // The frame is measured here so the remote screen size proposal and the
  // presentation crop both derive from the same real element box.
  const frameRef = useRef<HTMLDivElement | null>(null)
  const frameSize = useElementSize(frameRef)
  const presentationProfile = findPresentationProfile(provider)
  const proposedScreen = presentationProfile
    ? remoteScreenSizeForFrame(frameSize, presentationProfile, {
        maxWidth: configQuery.data?.max_screen_width,
        maxHeight: configQuery.data?.max_screen_height,
      })
    : null
  const connection = resolveConnection({
    sessionState: session?.state,
    surfaceStatus: surface.status,
    surfaceEnabled: isDesktop && isLive && Boolean(session?.session_id),
  })

  const projectsError = projectsQuery.isError
    ? classifyWebWorkspaceError(projectsQuery.error)
    : null
  const startError = startMutation.error
    ? classifyWebWorkspaceError(startMutation.error)
    : null
  const isNavigationAvailable =
    isLive && Boolean(session) && surface.status === 'connected'

  function navigate(action: WebWorkspaceNavigationAction, projectId?: number) {
    if (!session) return
    navigationMutation.mutate({
      sessionId: session.session_id,
      action,
      projectId,
    })
  }

  function openProject(project: WebProject) {
    setDrawerOpen(false)
    const navigationKey = session ? `${session.session_id}:${project.id}` : null
    if (!navigationKey || navigatedProjectRef.current !== navigationKey) {
      setProjectNavigationPending(true)
    }
    setSelectedProjectId(project.id)
    setProjectNavigationErrorKey(null)
  }

  useEffect(() => {
    if (firstProjectRequired) {
      if (!firstProjectPromptedRef.current) {
        firstProjectPromptedRef.current = true
        setCreateOpen(true)
      }
      return
    }
    firstProjectPromptedRef.current = false
  }, [firstProjectRequired])

  useEffect(() => {
    if (!projectsLoaded || selectedProjectId === null) return
    if (projects.some((project) => project.id === selectedProjectId)) return
    setSelectedProjectId(null)
    navigatedProjectRef.current = null
    setProjectNavigationErrorKey(null)
  }, [projects, projectsLoaded, selectedProjectId])

  useEffect(() => {
    if (
      !ready ||
      !isLive ||
      !session ||
      !projectsLoaded ||
      projects.length === 0 ||
      selectedProjectId !== null
    ) {
      return
    }
    const key = session.session_id
    if (autoOpenAttemptRef.current === key) return
    autoOpenAttemptRef.current = key
    const target = resolveAutoOpenProject(projects, readLastProjectId(userId))
    if (!target) return
    setProjectNavigationPending(true)
    setSelectedProjectId(target.id)
  }, [
    isLive,
    projects,
    projectsLoaded,
    ready,
    selectedProjectId,
    session,
    userId,
  ])

  useEffect(() => {
    if (!isLive || !session || !selectedProject) return
    const key = `${session.session_id}:${selectedProject.id}`
    if (navigatedProjectRef.current === key) return
    navigatedProjectRef.current = key
    setProjectNavigationPending(true)
    setProjectNavigationErrorKey(null)
    navigationMutation.mutate(
      {
        sessionId: session.session_id,
        action: 'project',
        projectId: selectedProject.id,
      },
      {
        onSuccess: () => {
          rememberLastProjectId(userId, selectedProject.id)
          setProjectNavigationPending(false)
        },
        onError: (navigationError) => {
          setProjectNavigationPending(false)
          setProjectNavigationErrorKey(
            classifyWebWorkspaceError(navigationError).messageKey
          )
        },
      }
    )
  }, [isLive, navigationMutation, selectedProject, session, userId])

  useEffect(() => {
    autoOpenAttemptRef.current = null
    navigatedProjectRef.current = null
    setProjectNavigationPending(false)
    setProjectNavigationErrorKey(null)
  }, [session?.session_id])

  function startSession() {
    startMutation.mutate({ screenSize: proposedScreen })
  }

  function restartSession() {
    if (!session) return
    restartMutation.mutate({
      sessionId: session.session_id,
      screenSize: proposedScreen,
    })
  }

  function lockSession() {
    if (!session) return
    restartMutation.mutate({
      sessionId: session.session_id,
      mode: 'LOCKED',
      screenSize: proposedScreen,
    })
  }

  if (configQuery.isPending) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Web Workspace')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <p className='text-muted-foreground flex items-center gap-2 py-8 text-sm'>
            <Spinner className='size-4 motion-reduce:animate-none' />
            {t('Loading Web Workspace...')}
          </p>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  if (configQuery.isError || !configQuery.data) {
    const error = configQuery.isError
      ? classifyWebWorkspaceError(configQuery.error)
      : null
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Web Workspace')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <Alert variant='destructive' role='alert' className='mt-4 max-w-xl'>
            <AlertTitle>{t('Could not load Web Workspace')}</AlertTitle>
            <AlertDescription>
              {t(
                error?.messageKey ?? 'Something went wrong. Please try again.'
              )}
            </AlertDescription>
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={() => {
                void configQuery.refetch()
              }}
            >
              {t('Retry')}
            </Button>
          </Alert>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  if (access !== 'ready') {
    return <WebWorkspaceDisabled state={access ?? 'not-entitled'} />
  }

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Web Workspace')}</SectionPageLayout.Title>
      {immersive.immersive ? null : (
        <SectionPageLayout.Center>
          <ProjectRail
            projects={projects}
            selectedProjectId={selectedProjectId}
            isLoading={projectsQuery.isPending}
            isError={projectsQuery.isError}
            canCreate={isLive}
            onSelect={openProject}
            onCreate={() => setCreateOpen(true)}
            onOpenManager={() => setDrawerOpen(true)}
          />
        </SectionPageLayout.Center>
      )}
      <SectionPageLayout.Actions>
        <WorkspaceToolbar
          connection={connection}
          connectionDetail={t(
            'Session {{state}} · stream {{stream}} · sent {{bytes}}',
            {
              state: session?.state ?? t('Not started'),
              stream: surface.status,
              bytes: formatByteSize(session?.stream_bytes_out ?? 0),
            }
          )}
          sessionMode={session?.mode ?? null}
          isLive={isLive}
          isBusy={isBusy}
          navigation={navigation}
          isNavigationAvailable={isNavigationAvailable}
          isNavigationPending={isBusy}
          immersive={immersive.immersive}
          onStart={startSession}
          onReconnect={surface.reconnect}
          onRestart={restartSession}
          onLockSession={lockSession}
          onStop={() => {
            if (session) stopMutation.mutate(session.session_id)
          }}
          onNavigateBack={() => navigate('back')}
          onNavigateForward={() => navigate('forward')}
          onNavigateReload={() => navigate('reload')}
          onToggleImmersive={immersive.toggle}
        />
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div
          data-testid='web-workspace-content'
          className='relative flex h-full min-h-0'
        >
          <RemoteBrowserViewport
            provider={provider}
            sessionId={session?.session_id ?? null}
            sessionState={session?.state}
            enabled={isDesktop}
            projectCreationFailed={projectCreationFailed}
            suppressStream={suppressRemoteStream}
            projectNavigationErrorKey={projectNavigationErrorKey}
            onRetryProject={() => {
              if (!selectedProject) return
              navigatedProjectRef.current = null
              openProject(selectedProject)
            }}
            projectsEmpty={
              !projectsQuery.isPending &&
              !projectsQuery.isError &&
              projects.length === 0
            }
            immersive={immersive.immersive}
            surface={surface}
            page={session?.page ?? null}
            startErrorMessageKey={startError?.messageKey ?? null}
            isReloadPending={navigationMutation.isPending}
            frameRef={frameRef}
            frameSize={frameSize}
            onStart={startSession}
            onReload={() => navigate('reload')}
            onExitImmersive={immersive.exit}
          />

          {immersive.immersive ? (
            <div
              data-testid='web-workspace-immersive-controls'
              className='bg-background/85 absolute top-3 right-3 z-20 flex items-center gap-2 rounded-full border p-1 shadow-sm backdrop-blur-sm'
            >
              <WorkspaceNavigationControls
                navigation={navigation}
                isAvailable={isNavigationAvailable}
                isPending={isBusy}
                onBack={() => navigate('back')}
                onForward={() => navigate('forward')}
                onReload={() => navigate('reload')}
              />
              <Button
                type='button'
                size='icon-sm'
                variant='ghost'
                aria-label={t('Exit immersive mode')}
                onClick={immersive.exit}
              >
                <Minimize2 aria-hidden='true' />
              </Button>
            </div>
          ) : null}
        </div>

        <ProjectDrawer
          open={drawerOpen}
          onOpenChange={setDrawerOpen}
          projects={projects}
          isLoading={projectsQuery.isPending}
          errorMessageKey={projectsError?.messageKey ?? null}
          selectedProjectId={selectedProjectId}
          canCreate={isLive}
          removalNames={removal.removedNames}
          onDismissRemoval={removal.dismiss}
          onRefetch={() => {
            void projectsQuery.refetch()
          }}
          onSelect={openProject}
          onCreate={() => setCreateOpen(true)}
          onRename={setRenameTarget}
          onDelete={setDeleteTarget}
        />

        <ProjectCreateDialog
          open={createOpen}
          onOpenChange={setCreateOpen}
          required={firstProjectRequired}
          screenSize={proposedScreen}
          onCreated={(project) => {
            setProjectNavigationPending(true)
            setSelectedProjectId(project.id)
            setCreateOpen(false)
          }}
        />
        <ProjectRenameDialog
          key={projectDialogKey('rename', renameTarget)}
          project={renameTarget}
          onOpenChange={(open) => {
            if (!open) setRenameTarget(null)
          }}
          onRemoved={removal.reportRemoved}
        />
        <ProjectDeleteDialog
          key={projectDialogKey('delete', deleteTarget)}
          project={deleteTarget}
          isLastProject={projects.length <= 1}
          onCreate={() => {
            setDeleteTarget(null)
            setCreateOpen(true)
          }}
          onOpenChange={(open) => {
            if (!open) setDeleteTarget(null)
          }}
          onDeleted={removal.markExpectedRemoval}
          onDeleteRejected={removal.clearExpectedRemoval}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
