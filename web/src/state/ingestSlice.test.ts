import { describe, it, expect, beforeEach, vi } from 'vitest';

import type { Plan, ProposedPlan, Trip } from '../api/types';

vi.mock('../api/client', () => ({
  ApiError: class {},
  api: {
    ingest: vi.fn(),
    ingestConfirm: vi.fn(),
    getTrip: vi.fn(),
  },
}));

import { api } from '../api/client';
import { useStore } from './store';

const mockApi = api as unknown as Record<string, ReturnType<typeof vi.fn>>;

function tripWithPlans(id: number): Trip & { plans: Plan[] } {
  return {
    id,
    name: 'NYC',
    destination: 'New York',
    my_role: 'owner',
    members: [],
    tags: [],
    created_at: '',
    updated_at: '',
    plans: [],
  } as Trip & { plans: Plan[] };
}

const proposal = { type: 'flight' } as unknown as ProposedPlan;

beforeEach(() => {
  vi.clearAllMocks();
  useStore.setState(
    { ingestProposals: [], ingestTripId: null, ingestBusy: false, currentTrip: null, error: null },
    false,
  );
});

describe('ingest', () => {
  it('stashes proposals on success', async () => {
    mockApi.ingest.mockResolvedValue({ proposals: [proposal] });
    await useStore.getState().ingest(5, { kind: 'paste', text: 'x' } as never);
    const s = useStore.getState();
    expect(mockApi.ingest).toHaveBeenCalledWith(5, { kind: 'paste', text: 'x' });
    expect(s.ingestProposals).toHaveLength(1);
    expect(s.ingestTripId).toBe(5);
    expect(s.ingestBusy).toBe(false);
  });

  it('sets error, clears busy and rethrows on failure', async () => {
    mockApi.ingest.mockRejectedValue(new Error('bad'));
    await expect(useStore.getState().ingest(5, { kind: 'paste' } as never)).rejects.toThrow('bad');
    const s = useStore.getState();
    expect(s.error).toBe('bad');
    expect(s.ingestBusy).toBe(false);
  });

  it('stringifies a non-Error rejection', async () => {
    mockApi.ingest.mockRejectedValue('strerr');
    await expect(useStore.getState().ingest(5, {} as never)).rejects.toBe('strerr');
    expect(useStore.getState().error).toBe('strerr');
  });
});

describe('confirmIngest', () => {
  it('clears proposals and reloads the trip when it is open', async () => {
    useStore.setState({
      ingestProposals: [proposal],
      ingestTripId: 5,
      currentTrip: tripWithPlans(5),
    });
    mockApi.ingestConfirm.mockResolvedValue(undefined);
    mockApi.getTrip.mockResolvedValue(tripWithPlans(5));
    await useStore.getState().confirmIngest(5, [{ type: 'flight' }] as never);
    const s = useStore.getState();
    expect(mockApi.ingestConfirm).toHaveBeenCalledWith(5, [{ type: 'flight' }]);
    expect(s.ingestProposals).toEqual([]);
    expect(s.ingestTripId).toBeNull();
    expect(s.ingestBusy).toBe(false);
    expect(mockApi.getTrip).toHaveBeenCalledWith(5);
  });

  it('does not reload when a different trip is open', async () => {
    useStore.setState({ currentTrip: tripWithPlans(99) });
    mockApi.ingestConfirm.mockResolvedValue(undefined);
    await useStore.getState().confirmIngest(5, [] as never);
    expect(mockApi.getTrip).not.toHaveBeenCalled();
  });

  it('sets error, clears busy and rethrows on failure', async () => {
    mockApi.ingestConfirm.mockRejectedValue(new Error('bad'));
    await expect(useStore.getState().confirmIngest(5, [] as never)).rejects.toThrow('bad');
    const s = useStore.getState();
    expect(s.error).toBe('bad');
    expect(s.ingestBusy).toBe(false);
  });
});

describe('clearIngest', () => {
  it('discards pending proposals', () => {
    useStore.setState({ ingestProposals: [proposal], ingestTripId: 5 });
    useStore.getState().clearIngest();
    const s = useStore.getState();
    expect(s.ingestProposals).toEqual([]);
    expect(s.ingestTripId).toBeNull();
  });
});
