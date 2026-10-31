# Asynchronous Standby & Liveness Patrol Rules

> This file is part of CEO Skill. Read when: transitioning to STANDBY after delegating a task, handling wake-up scenarios, performing routine liveness snapshots, or issuing anti-loop corrections.

---

## 1. Asynchronous Standby Principle (Anti-Polling Iron Rule)

* **(MUST KNOW)** To prevent context bloat and rule amnesia, CEO operates strictly on an **Asynchronous / Event-Driven** model. CEO DOES NOT poll real-time terminal outputs, line-by-line tool logs, or intermediate scratch commands of subagents.
* **(MUST)** After assigning a complete contract packet to a subagent lane and receiving the mandatory immediate ACK `UNDERSTOOD`, CEO MUST immediately transition into a **STANDBY** state (ending the turn without calling redundant tools).
* **(MUST NOT)** Repeatedly polling git status, re-running health check commands, or inspecting worker logs/terminals while the subagent is working is strictly prohibited.

---

## 2. The Three Wake-Up Scenarios

### SCENARIO A: Subagent Reports Early (< 10 mins)
* When a subagent finishes work and sends a direct message (`PASS`, `READY_FOR_<Role>`, `BLOCKED`), this event wakes CEO immediately.
* CEO automatically cancels the scheduled heartbeat timer and triggers the next workflow transition (see `references/subagent-reporting-and-handoff.md`).
* After assigning work to the next subagent lane, CEO sets a new 10-minute one-shot wake-up timer (see `references/timer-rules.md`) and transitions back into the **STANDBY** state.

### SCENARIO B: 10-Minute Timer Scheduled Heatbeat Fires (Periodic Liveness Check)
* If no message has been received after 10 minutes, the timer heartbeat wakes CEO to perform a **1-Step Bounded Health Check**:
  1. **Recent Delta Snapshot Only (Anti-Context-Bloat):** CEO inspects ONLY a single snapshot of the subagent's recent activity to distinguish between: *active healthy progress*, *technical blocker*, or *actual document audit loop / scope deviation*. CEO is STRICTLY FORBIDDEN from reading the entire long execution history of the subagent.
  2. **Healthy Progress (Quiet Standby):** As long as the subagent is actively making tangible progress (writing code, compiling, executing tests, deep refactoring) without loop symptoms, CEO MUST NOT send distracting messages and MUST NOT interrupt the lane. CEO simply resets the 10-minute timer (consulting `references/timer-rules.md`) and returns to STANDBY immediately.
  3. **Loop Detection & Anti-Loop Warning:** If detected falling into a Document Audit Loop or senseless command loop: Immediately issue a warning to the subagent, then reset the 10-minute timer and return to STANDBY.

### SCENARIO C: Message Received During Patrol (Event Preemption)
* If the subagent sends a report message while CEO is performing the Scenario B health check, CEO immediately **halts the routine check**, prioritizes processing the subagent's report to advance the workflow according to protocol, and re-establishes the timed STANDBY state for the next lane.

---

## 3. Transition & Timer Setup Mandate

* **(MUST)** Prior to ending the turn and entering the STANDBY state, CEO MUST consult [`references/timer-rules.md`](timer-rules.md) to arm the mandatory 10-minute liveness heartbeat timer via the `schedule` tool.
* Transitioning to STANDBY without setting the liveness timer is strictly prohibited.
