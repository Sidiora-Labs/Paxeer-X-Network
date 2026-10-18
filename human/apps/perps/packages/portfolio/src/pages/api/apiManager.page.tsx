import { APIManagerWidget } from "./apiManager.widget";

/**
 * API key manager page
 * @param filterTags filterTags
 * @param keyStatus filterTags default is "ACTIVE"
 * @returns
 */
export const APIManagerPage = (props: {
  filterTags?: [string];
  keyStatus?: string;
}) => {
  const { filterTags, keyStatus = "ACTIVE" } = props;
  return <APIManagerWidget filterTags={filterTags} keyStatus={keyStatus}/>;
};
