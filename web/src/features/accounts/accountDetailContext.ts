import { useOutletContext } from "react-router-dom";

import type { Account } from "../../api/schemas";

export type AccountDetailContextValue = {
  account: Account;
};

export function useAccountDetailContext() {
  return useOutletContext<AccountDetailContextValue>();
}
