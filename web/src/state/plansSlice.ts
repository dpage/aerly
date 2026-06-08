import type { StateCreator } from 'zustand';

import { api } from '../api/client';
import type {
  CreatePlanInput,
  NotifySharesInput,
  PlanPart,
  PlanVisibility,
  UpdatePlanInput,
  UpdatePlanPartInput,
} from '../api/types';
import { reloadCurrent } from './helpers';
import type { StoreState } from './store';

/** State + actions for creating and editing plans and their parts.
 *
 * Wave 0b: thin "call client, then reload the current trip" stubs. The plan
 * mutations all live on a trip, so the simplest correct behaviour is to
 * refresh `currentTrip` after each write. Wave 1F replaces this with
 * in-place reconciliation and SSE handling. */
export interface PlansSlice {
  createPlan: (tripId: number, input: CreatePlanInput) => Promise<void>;
  updatePlan: (planId: number, patch: UpdatePlanInput) => Promise<void>;
  deletePlan: (planId: number) => Promise<void>;
  addPlanPassenger: (planId: number, userId: number) => Promise<void>;
  removePlanPassenger: (planId: number, userId: number) => Promise<void>;
  setPlanVisibility: (planId: number, visibility: PlanVisibility) => Promise<void>;
  movePlan: (planId: number, toTripId: number) => Promise<void>;
  linkPlans: (primaryId: number, planIds: number[]) => Promise<void>;
  splitPlanPart: (partId: number) => Promise<void>;
  updatePlanPart: (partId: number, patch: UpdatePlanPartInput) => Promise<PlanPart>;
  dismissPlanPart: (partId: number) => Promise<void>;
  setPlanShareAllFriends: (planId: number, enabled: boolean) => Promise<void>;
  notifyPlanShares: (planId: number, input: NotifySharesInput) => Promise<void>;
}


export const createPlansSlice: StateCreator<StoreState, [], [], PlansSlice> = (_set, get) => ({
  async createPlan(tripId, input) {
    await api.createPlan(tripId, input);
    if (get().currentTrip?.id === tripId) await reloadCurrent(get);
  },

  async updatePlan(planId, patch) {
    await api.updatePlan(planId, patch);
    await reloadCurrent(get);
  },

  async deletePlan(planId) {
    await api.deletePlan(planId);
    await reloadCurrent(get);
  },

  async addPlanPassenger(planId, userId) {
    await api.addPlanPassenger(planId, userId);
    await reloadCurrent(get);
  },

  async removePlanPassenger(planId, userId) {
    await api.removePlanPassenger(planId, userId);
    await reloadCurrent(get);
  },

  async setPlanVisibility(planId, visibility) {
    await api.setPlanVisibility(planId, visibility);
    await reloadCurrent(get);
  },

  async movePlan(planId, toTripId) {
    await api.movePlan(planId, { trip_id: toTripId });
    await reloadCurrent(get);
  },

  async linkPlans(primaryId, planIds) {
    await api.linkPlans(primaryId, { plan_ids: planIds });
    await reloadCurrent(get);
  },

  async splitPlanPart(partId) {
    await api.splitPlanPart(partId);
    await reloadCurrent(get);
  },

  async updatePlanPart(partId, patch) {
    const updated = await api.updatePlanPart(partId, patch);
    await reloadCurrent(get);
    return updated;
  },

  async dismissPlanPart(partId) {
    await api.dismissPlanPart(partId);
    await reloadCurrent(get);
  },

  async setPlanShareAllFriends(planId, enabled) {
    await api.setPlanShareAllFriends(planId, enabled);
    await reloadCurrent(get);
  },

  async notifyPlanShares(planId, input) {
    await api.notifyPlanShares(planId, input);
  },
});
