import { DevChecks } from '../../../../../dev/DevPage';
import { getHealth } from '../../api/request';
export function DevPage() { return <DevChecks app="merchant" load={getHealth} />; }
