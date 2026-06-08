import { describe, it, expect, beforeEach, vi } from 'vitest';

import type { Plan, Trip } from '../api/types';

vi.mock('../api/client', () => ({
  ApiError: class {},
  api: {
    createPlan: vi.fn(),
    updatePlan: vi.fn(),
    deletePlan: vi.fn(),
    addPlanPassenger: vi.fn(),
    removePlanPassenger: vi.fn(),
    setPlanVisibility: vi.fn(),
    movePlan: vi.fn(),
    linkPlans: vi.fn(),
    splitPlanPart: vi.fn(),
    updatePlanPart: vi.fn(),
    dismissPlanPart: vi.fn(),
    setPlanShareAllFriends: vi.fn(),
    notifyPlanShares: vi.fn(),
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

beforeEach(() => {
  vi.clearAllMocks();
  useStore.setState({ currentTrip: null, error: null }, false);
});

describe('createPlan', () => {
  it('reloads the current trip when the open trip matches', async () => {
    useStore.setState({ currentTrip: tripWithPlans(7) });
    mockApi.createPlan.mockResolvedValue(undefined);
    mockApi.getTrip.mockResolvedValue(tripWithPlans(7));
    await useStore.getState().createPlan(7, { type: 'flight' } as never);
    expect(mockApi.createPlan).toHaveBeenCalledWith(7, { type: 'flight' });
    expect(mockApi.getTrip).toHaveBeenCalledWith(7);
  });

  it('does not reload when the created plan targets a different trip', async () => {
    useStore.setState({ currentTrip: tripWithPlans(7) });
    mockApi.createPlan.mockResolvedValue(undefined);
    await useStore.getState().createPlan(99, { type: 'flight' } as never);
    expect(mockApi.getTrip).not.toHaveBeenCalled();
  });

  it('does not reload when no trip is open', async () => {
    mockApi.createPlan.mockResolvedValue(undefined);
    await useStore.getState().createPlan(7, { type: 'flight' } as never);
    expect(mockApi.getTrip).not.toHaveBeenCalled();
  });
});

describe('mutations that reload the current trip', () => {
  beforeEach(() => {
    useStore.setState({ currentTrip: tripWithPlans(7) });
    mockApi.getTrip.mockResolvedValue(tripWithPlans(7));
  });

  it('updatePlan reloads', async () => {
    mockApi.updatePlan.mockResolvedValue(undefined);
    await useStore.getState().updatePlan(1, { title: 'x' } as never);
    expect(mockApi.updatePlan).toHaveBeenCalledWith(1, { title: 'x' });
    expect(mockApi.getTrip).toHaveBeenCalledWith(7);
  });

  it('deletePlan reloads', async () => {
    mockApi.deletePlan.mockResolvedValue(undefined);
    await useStore.getState().deletePlan(1);
    expect(mockApi.deletePlan).toHaveBeenCalledWith(1);
    expect(mockApi.getTrip).toHaveBeenCalled();
  });

  it('addPlanPassenger reloads', async () => {
    mockApi.addPlanPassenger.mockResolvedValue(undefined);
    await useStore.getState().addPlanPassenger(1, 2);
    expect(mockApi.addPlanPassenger).toHaveBeenCalledWith(1, 2);
    expect(mockApi.getTrip).toHaveBeenCalled();
  });

  it('removePlanPassenger reloads', async () => {
    mockApi.removePlanPassenger.mockResolvedValue(undefined);
    await useStore.getState().removePlanPassenger(1, 2);
    expect(mockApi.removePlanPassenger).toHaveBeenCalledWith(1, 2);
    expect(mockApi.getTrip).toHaveBeenCalled();
  });

  it('setPlanVisibility reloads', async () => {
    mockApi.setPlanVisibility.mockResolvedValue(undefined);
    await useStore.getState().setPlanVisibility(1, { mode: 'everyone', user_ids: [] });
    expect(mockApi.setPlanVisibility).toHaveBeenCalledWith(1, { mode: 'everyone', user_ids: [] });
    expect(mockApi.getTrip).toHaveBeenCalled();
  });

  it('movePlan calls the client with the trip wrapper and reloads', async () => {
    mockApi.movePlan.mockResolvedValue(undefined);
    await useStore.getState().movePlan(1, 42);
    expect(mockApi.movePlan).toHaveBeenCalledWith(1, { trip_id: 42 });
    expect(mockApi.getTrip).toHaveBeenCalled();
  });

  it('linkPlans calls the client with the plan_ids wrapper and reloads', async () => {
    mockApi.linkPlans.mockResolvedValue(undefined);
    await useStore.getState().linkPlans(1, [2, 3]);
    expect(mockApi.linkPlans).toHaveBeenCalledWith(1, { plan_ids: [2, 3] });
    expect(mockApi.getTrip).toHaveBeenCalled();
  });

  it('splitPlanPart calls the client and reloads', async () => {
    mockApi.splitPlanPart.mockResolvedValue(undefined);
    await useStore.getState().splitPlanPart(5);
    expect(mockApi.splitPlanPart).toHaveBeenCalledWith(5);
    expect(mockApi.getTrip).toHaveBeenCalled();
  });

  it('updatePlanPart reloads', async () => {
    mockApi.updatePlanPart.mockResolvedValue(undefined);
    await useStore.getState().updatePlanPart(1, { status: 'x' } as never);
    expect(mockApi.updatePlanPart).toHaveBeenCalledWith(1, { status: 'x' });
    expect(mockApi.getTrip).toHaveBeenCalled();
  });

  it('dismissPlanPart reloads', async () => {
    mockApi.dismissPlanPart.mockResolvedValue(undefined);
    await useStore.getState().dismissPlanPart(1);
    expect(mockApi.dismissPlanPart).toHaveBeenCalledWith(1);
    expect(mockApi.getTrip).toHaveBeenCalled();
  });

  it('setPlanShareAllFriends calls the client and reloads', async () => {
    mockApi.setPlanShareAllFriends.mockResolvedValue(undefined);
    await useStore.getState().setPlanShareAllFriends(1, true);
    expect(mockApi.setPlanShareAllFriends).toHaveBeenCalledWith(1, true);
    expect(mockApi.getTrip).toHaveBeenCalled();
  });
});

describe('reloadCurrent no-op when no trip open', () => {
  it('updatePlan does not call getTrip when no trip is open', async () => {
    mockApi.updatePlan.mockResolvedValue(undefined);
    await useStore.getState().updatePlan(1, { title: 'x' } as never);
    expect(mockApi.getTrip).not.toHaveBeenCalled();
  });
});

describe('notifyPlanShares', () => {
  it('calls the client with the given planId and input', async () => {
    mockApi.notifyPlanShares.mockResolvedValue(undefined);
    const input = { user_ids: [3], emails: ['x@y.com'] };
    await useStore.getState().notifyPlanShares(5, input);
    expect(mockApi.notifyPlanShares).toHaveBeenCalledWith(5, input);
  });
});
