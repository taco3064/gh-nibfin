import { createContext, useContext } from 'react'

import { complete, type Loading } from '@/lib/partial'

// LoadingContext carries what the shown read is still loading to the
// sections below it, such as the Issue rows; complete by default.
export const LoadingContext = createContext<Loading>(complete)

export function useLoading(): Loading {
  return useContext(LoadingContext)
}
