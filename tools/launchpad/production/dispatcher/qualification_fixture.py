"""Private qualification fixture state; never applied to the canonical ledger."""
import copy
import hashlib
import re


def initial_states(tasks):
    return {task: 'done' if task in ('0.1', '0.2', '0.3') else 'pending'
            for task in tasks}


def normalize_fixture(text, plan):
    """Copy and normalize status projections only; preserve graph/layout fields."""
    states = initial_states(plan['tasks'])
    def replace(match):
        task, section = match.group(1), match.group(0)
        return re.sub(r'^status = ".*"$', 'status = "'+states[task]+'"',
                      section, flags=re.M)
    text = re.sub(r'^\[task\.([^\]]+)\]\n.*?(?=^\[|\Z)', replace,
                  text, flags=re.M | re.S)
    plan = copy.deepcopy(plan)
    for task, status in states.items():
        plan['tasks'][task]['status'] = status
    for lane in plan['lanes']:
        for task in lane['tasks']:
            task['status'] = states[task['id']]
    plan['spec_sha256'] = hashlib.sha256(text.encode()).hexdigest()
    return text, plan
