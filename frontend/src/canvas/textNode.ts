export interface TextNodeData {
  kind: 'text'
  name: string
  text: string
}

export interface TextReference { id: string; name: string; text: string }

// The preview rectangle is also used by canvas alignment.
export const TEXT_NODE_WIDTH = 320
export const TEXT_NODE_HEIGHT = 240
