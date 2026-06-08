import { describe, expect, it, beforeEach } from 'vitest';
import { renderHook } from '@testing-library/react';

import { useStore } from './store';
import { useFriendUsers, useFriendCandidates } from './friendUsers';
import type { Friendship, User } from '../api/types';

function user(id: number, name = `user${id}`): User {
  return {
    id,
    username: name,
    email: `${name}@example.com`,
    is_superuser: false,
    is_active: true,
    avatar_url: '',
  } as User;
}

function friendship(other: number, status: Friendship['status']): Friendship {
  return {
    friend_id: other,
    status,
    requested_at: '2026-01-01T00:00:00Z',
  };
}

describe('useFriendUsers', () => {
  beforeEach(() => {
    useStore.setState({
      me: user(1) as never,
      users: [user(1), user(2), user(3), user(4)],
      friendships: [
        friendship(2, 'accepted'),
        friendship(3, 'pending'),
      ],
    });
  });

  it('returns only users with an accepted friendship to me', () => {
    const { result } = renderHook(() => useFriendUsers());
    expect(result.current.map((u) => u.id)).toEqual([2]);
  });

  it('returns an empty list when me is null', () => {
    useStore.setState({ me: null });
    const { result } = renderHook(() => useFriendUsers());
    expect(result.current).toEqual([]);
  });

  it('returns all accepted friends, in users-array order', () => {
    useStore.setState({
      friendships: [
        friendship(4, 'accepted'),
        friendship(2, 'accepted'),
      ],
    });
    const { result } = renderHook(() => useFriendUsers());
    // Order follows the users array (1, 2, 3, 4) not the friendships array,
    // because the selector filters users by membership in the friend set.
    expect(result.current.map((u) => u.id)).toEqual([2, 4]);
  });

  it('ignores pending and outgoing pending entries', () => {
    useStore.setState({
      friendships: [
        { ...friendship(2, 'pending'), direction: 'outgoing' },
        { ...friendship(3, 'pending'), direction: 'incoming' },
      ],
    });
    const { result } = renderHook(() => useFriendUsers());
    expect(result.current).toEqual([]);
  });
});

describe('useFriendCandidates', () => {
  beforeEach(() => {
    useStore.setState({
      me: user(1) as never,
      users: [user(1), user(2), user(3), user(4)],
      friendships: [],
    });
  });

  it('returns an accepted friendship as pending:false', () => {
    useStore.setState({
      friendships: [friendship(2, 'accepted')],
    });
    const { result } = renderHook(() => useFriendCandidates());
    expect(result.current).toEqual([{ user: user(2), pending: false }]);
  });

  it('returns a pending friendship (incoming) as pending:true', () => {
    useStore.setState({
      friendships: [{ ...friendship(3, 'pending'), direction: 'incoming' as const }],
    });
    const { result } = renderHook(() => useFriendCandidates());
    expect(result.current).toEqual([{ user: user(3), pending: true }]);
  });

  it('returns a pending friendship (outgoing with friend_id) as pending:true', () => {
    useStore.setState({
      friendships: [{ ...friendship(2, 'pending'), direction: 'outgoing' as const }],
    });
    const { result } = renderHook(() => useFriendCandidates());
    expect(result.current).toEqual([{ user: user(2), pending: true }]);
  });

  it('excludes outgoing email-only invites (friend_id undefined)', () => {
    useStore.setState({
      friendships: [
        {
          status: 'pending',
          email: 'stranger@example.com',
          direction: 'outgoing' as const,
          requested_at: '2026-01-01T00:00:00Z',
          // friend_id intentionally omitted
        },
      ],
    });
    const { result } = renderHook(() => useFriendCandidates());
    expect(result.current).toEqual([]);
  });

  it('returns [] when me is null', () => {
    useStore.setState({ me: null });
    const { result } = renderHook(() => useFriendCandidates());
    expect(result.current).toEqual([]);
  });
});
