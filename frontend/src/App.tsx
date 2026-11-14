import { lazy, Suspense, type ReactNode } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import { AppLayout } from '@/components/AppLayout'
import { Loading, RequireAuth } from '@/components/states'

// Each page loads on first visit; the editor bundle (Monaco) only with the problem page.
const LoginPage = lazy(() => import('@/pages/LoginPage'))
const RegisterPage = lazy(() => import('@/pages/RegisterPage'))
const ProblemsPage = lazy(() => import('@/pages/ProblemsPage'))
const ProblemPage = lazy(() => import('@/pages/ProblemPage'))
const SubmissionsPage = lazy(() => import('@/pages/SubmissionsPage'))
const SubmissionPage = lazy(() => import('@/pages/SubmissionPage'))
const LeaderboardPage = lazy(() => import('@/pages/LeaderboardPage'))
const ProfilePage = lazy(() => import('@/pages/ProfilePage'))
const AdminProblemsPage = lazy(() => import('@/pages/admin/AdminProblemsPage'))
const AdminProblemEditPage = lazy(() => import('@/pages/admin/AdminProblemEditPage'))
const AdminUsersPage = lazy(() => import('@/pages/admin/AdminUsersPage'))

function page(element: ReactNode) {
  return <Suspense fallback={<Loading />}>{element}</Suspense>
}

export function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<AppLayout />}>
          <Route index element={<Navigate to="/problems" replace />} />
          <Route path="login" element={page(<LoginPage />)} />
          <Route path="register" element={page(<RegisterPage />)} />
          <Route path="problems" element={page(<ProblemsPage />)} />
          <Route path="problems/:slug" element={page(<ProblemPage />)} />
          <Route path="leaderboard" element={page(<LeaderboardPage />)} />
          <Route path="submissions" element={page(<RequireAuth>{<SubmissionsPage />}</RequireAuth>)} />
          <Route path="submissions/:id" element={page(<RequireAuth>{<SubmissionPage />}</RequireAuth>)} />
          <Route path="profile" element={page(<RequireAuth>{<ProfilePage />}</RequireAuth>)} />
          <Route path="admin" element={<Navigate to="/admin/problems" replace />} />
          <Route
            path="admin/problems"
            element={page(<RequireAuth admin>{<AdminProblemsPage />}</RequireAuth>)}
          />
          <Route
            path="admin/problems/new"
            element={page(<RequireAuth admin>{<AdminProblemEditPage />}</RequireAuth>)}
          />
          <Route
            path="admin/problems/:id"
            element={page(<RequireAuth admin>{<AdminProblemEditPage />}</RequireAuth>)}
          />
          <Route path="admin/users" element={page(<RequireAuth admin>{<AdminUsersPage />}</RequireAuth>)} />
          <Route path="*" element={<Navigate to="/problems" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
