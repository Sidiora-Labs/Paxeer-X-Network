# pages/multiLevel/affiliate

## Directory Responsibility

Multi-level affiliate dashboard: page, summary, commission chart, referral info, referral code form (create/edit/reset), and referrer table (commission table, referees table, referral codes table). Uses multi-level API and useReferralCode.

## Key Files / Sections

| Section | Summary | Link |
|---------|---------|------|
| page.tsx | MultiLevelAffiliatePage | page.md |
| summary | Summary UI/widget/script | summary/ |
| commissionChart | Commission chart UI/widget/script | commissionChart/ |
| referralInfo | Referral info widget/UI/script | referralInfo/ |
| referralCodeForm | Form UI/widget/script, modal constants | referralCodeForm/ |
| referrerTable | ReferrerTable (tabs: commission, referees, codes), base cells | referrerTable/ |
| multiLevelReferral | MultiLevelReferral UI/widget/script | multiLevelReferral/ |

## Key Entities

| Entity | Responsibility |
|--------|----------------|
| MultiLevelAffiliatePage | Container for multi-level affiliate content |
| ReferralCodeForm | Create/edit/reset referral code |
| ReferrerTable | Commission, referees, and codes tables |
| CommissionChart | Volume/commission chart |
