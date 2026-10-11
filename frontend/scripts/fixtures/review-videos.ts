import { get } from './review-request'
import type { VideoData } from '../../src/types/videohub'
export { isVideoInitiallyAvailable } from '../../src/api/videohub/videoMapping'
export const fetchVideoList = () => get<VideoData[]>('/api/custom/videos')
export const fetchVideoDetail = (id: string) => get<VideoData>(`/api/custom/videos/${id}`)
